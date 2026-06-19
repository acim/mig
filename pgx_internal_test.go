package mig

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dsn = "postgres://postgres@localhost:5432/mig"

var (
	_ pgxConn  = (*pgx.Conn)(nil)
	_ pgxConn  = (*pgxpool.Conn)(nil)
	_ Database = (*pgxDB)(nil)
)

func TestPgxLockIDUsesCanonicalTableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tableName string
	}{
		{
			name:      "default",
			tableName: "schema_migrations",
		},
		{
			name:      "schema qualified",
			tableName: "app.schema_migrations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			const database = "mig"
			const schema = "public"
			db := newPgxDB(lockIdentityConn{database: database, schema: schema}, tt.tableName)

			if err := db.setLockID(context.Background()); err != nil {
				t.Fatalf("setLockID(): %v", err)
			}

			want := expectedPgxLockID(database, schema, tt.tableName)
			if db.lockID != want {
				t.Fatalf("lockID=%s; want lock ID hashed from canonical table name %s", db.lockID, want)
			}
		})
	}
}

func TestPgxMigrateRollsBackMigrationWhenVersionRecordingFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	tableName := testTableName(t, "atomic_versions")
	sideEffectTable := testTableName(t, "atomic_side_effects")
	pool := pgxPool(ctx, t)
	dropTable(ctx, t, pool, tableName)
	dropTable(ctx, t, pool, sideEffectTable)
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+" (version bigint PRIMARY KEY)"); err != nil {
		t.Fatalf("create migration table %s: %v", tableName, err)
	}
	t.Cleanup(func() {
		dropTable(ctx, t, pool, tableName)
		dropTable(ctx, t, pool, sideEffectTable)
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	migrator := New(Migrations{{
		Version: 1,
		Path:    "001-break-version-recording.sql",
		SQL: fmt.Sprintf(
			"CREATE TABLE %s (id integer); DROP TABLE %s",
			sideEffectTable,
			tableName,
		),
	}}, newPgxDB(newPgxPoolConn(conn), tableName))

	err = migrator.Migrate(ctx)
	if err == nil {
		t.Fatal("Migrate() error=<nil>; want version recording error")
	}

	if tableExists(ctx, t, pool, sideEffectTable) {
		t.Fatalf("side effect table %s exists; want migration transaction rolled back", sideEffectTable)
	}

	if !tableExists(ctx, t, pool, tableName) {
		t.Fatalf("migration table %s does not exist; want rollback to restore it", tableName)
	}
}

func TestPgxMigrateUsesTransactionScopedAdvisoryLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	tableName := testTableName(t, "lock_cleanup")
	lockCountTable := testTableName(t, "lock_counts")
	pool := pgxPool(ctx, t)
	dropTable(ctx, t, pool, tableName)
	dropTable(ctx, t, pool, lockCountTable)
	t.Cleanup(func() {
		dropTable(ctx, t, pool, tableName)
		dropTable(ctx, t, pool, lockCountTable)
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	migrator := New(Migrations{{
		Version: 1,
		Path:    "001-check-lock.sql",
		SQL: fmt.Sprintf(`
			CREATE TABLE %s (lock_count integer NOT NULL);
			SELECT pg_advisory_unlock_all();
			INSERT INTO %s (lock_count)
			SELECT count(*)
			FROM pg_locks
			WHERE locktype = 'advisory'
				AND pid = pg_backend_pid();
		`, lockCountTable, lockCountTable),
	}}, newPgxDB(newPgxPoolConn(conn), tableName))

	if err := migrator.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}

	var lockCount int
	if err := pool.QueryRow(ctx, "SELECT lock_count FROM "+lockCountTable).Scan(&lockCount); err != nil {
		t.Fatalf("read lock count: %v", err)
	}

	if lockCount == 0 {
		t.Fatal("migration observed no advisory lock after pg_advisory_unlock_all; want transaction-scoped advisory lock")
	}
}

func TestPgxMigratePreservesPgError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	tableName := testTableName(t, "pg_error")
	pool := pgxPool(ctx, t)
	dropTable(ctx, t, pool, tableName)
	t.Cleanup(func() {
		dropTable(ctx, t, pool, tableName)
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	const query = "CREATE TABLE"
	migrator := New(Migrations{{
		Version: 1,
		Path:    "001-broken.sql",
		SQL:     query,
	}}, newPgxDB(newPgxPoolConn(conn), tableName))

	err = migrator.Migrate(ctx)

	var got *pgconn.PgError
	if !errors.As(err, &got) {
		t.Fatalf("Migrate() error=%v; want pg error", err)
	}

	if got.SQLState() != "42601" {
		t.Fatalf("PgError.SQLState()=%q; want %q", got.SQLState(), "42601")
	}

	for _, want := range []string{
		"run migration 1 from file 001-broken.sql",
		"execute migration SQL",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Migrate() error=%q; want %q", err, want)
		}
	}

	if strings.Contains(err.Error(), query) {
		t.Fatalf("Migrate() error=%q; should not include SQL query text", err)
	}
}

func TestPgxMigrateWrapsCreateTableError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	schemaName := testTableName(t, "missing_schema")
	pool := pgxPool(ctx, t)
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); err != nil {
		t.Fatalf("drop schema before test: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); err != nil {
			t.Errorf("drop schema after test: %v", err)
		}
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	migrator := New(nil, newPgxDB(newPgxPoolConn(conn), schemaName+".schema_migrations"))

	err = migrator.Migrate(ctx)
	if err == nil {
		t.Fatal("Migrate() error=<nil>; want create table error")
	}

	if !strings.Contains(err.Error(), "create schema migrations table") {
		t.Fatalf("Migrate() error=%q; want create table context", err)
	}
}

func TestPgxMigrateWrapsLastVersionScanError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	tableName := testTableName(t, "bad_versions")
	pool := pgxPool(ctx, t)
	dropTable(ctx, t, pool, tableName)
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+" (version text PRIMARY KEY)"); err != nil {
		t.Fatalf("create bad migration table: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+tableName+" (version) VALUES ('not-a-number')"); err != nil {
		t.Fatalf("insert bad migration version: %v", err)
	}
	t.Cleanup(func() {
		dropTable(ctx, t, pool, tableName)
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	migrator := New(nil, newPgxDB(newPgxPoolConn(conn), tableName))

	err = migrator.Migrate(ctx)
	if err == nil {
		t.Fatal("Migrate() error=<nil>; want last version scan error")
	}

	if !strings.Contains(err.Error(), "last version") {
		t.Fatalf("Migrate() error=%q; want last version context", err)
	}
}

func TestPgxMigrateWrapsSetLockIDError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close connection: %v", err)
	}

	migrator := New(nil, newPgxDB(newPgxConn(conn), testTableName(t, "closed_conn")))

	err = migrator.Migrate(ctx)
	if err == nil {
		t.Fatal("Migrate() error=<nil>; want set lock id error")
	}

	if !strings.Contains(err.Error(), "set lock id") {
		t.Fatalf("Migrate() error=%q; want set lock id context", err)
	}
}

func TestFromPgxPoolReturnsInvalidTableNameError(t *testing.T) {
	t.Parallel()

	migrator, cleanup, err := FromPgxPool(nil, nil, WithCustomTable("bad name"))
	if !errors.Is(err, ErrInvalidTableName) {
		t.Fatalf("FromPgxPool() error=%v; want invalid table name error", err)
	}

	if migrator != nil {
		t.Fatalf("FromPgxPool() migrator=%v; want nil", migrator)
	}

	if cleanup != nil {
		t.Fatal("FromPgxPool() cleanup is not nil")
	}
}

func TestPgxMigrateRejectsZeroVersionMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	tableName := testTableName(t, "zero_versions")
	sideEffectTable := testTableName(t, "zero_side_effects")
	pool := pgxPool(ctx, t)
	dropTable(ctx, t, pool, tableName)
	dropTable(ctx, t, pool, sideEffectTable)
	t.Cleanup(func() {
		dropTable(ctx, t, pool, tableName)
		dropTable(ctx, t, pool, sideEffectTable)
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	migrator := New(Migrations{{
		Version: 0,
		Path:    "000-invalid.sql",
		SQL:     "CREATE TABLE " + sideEffectTable + " (id integer)",
	}}, newPgxDB(newPgxPoolConn(conn), tableName))

	err = migrator.Migrate(ctx)
	if !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("Migrate() error=%v; want invalid version error", err)
	}

	if tableExists(ctx, t, pool, sideEffectTable) {
		t.Fatalf("side effect table %s exists; want zero-version migration rejected before execution", sideEffectTable)
	}
}

func TestPgxMigrateWithSchemaQualifiedCustomTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	schemaName := testTableName(t, "custom_schema")
	tableName := testTableName(t, "custom_table")
	sideEffectTable := testTableName(t, "custom_side_effect")
	pool := pgxPool(ctx, t)
	dropTable(ctx, t, pool, sideEffectTable)
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); err != nil {
		t.Fatalf("drop schema before test: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		dropTable(ctx, t, pool, sideEffectTable)
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); err != nil {
			t.Errorf("drop schema after test: %v", err)
		}
	})

	migrator, cleanup, err := FromPgxPool(Migrations{{
		Version: 1,
		Path:    "001-custom-table.sql",
		SQL:     "CREATE TABLE " + sideEffectTable + " (id integer)",
	}}, pool, WithCustomTable(schemaName+"."+tableName))
	if err != nil {
		t.Fatalf("FromPgxPool(): %v", err)
	}
	defer cleanup()

	if err := migrator.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}

	if !tableExists(ctx, t, pool, sideEffectTable) {
		t.Fatalf("side effect table %s does not exist; want migration to run", sideEffectTable)
	}

	var version uint64
	if err := pool.QueryRow(ctx, "SELECT version FROM "+schemaName+"."+tableName).Scan(&version); err != nil {
		t.Fatalf("read custom schema migration version: %v", err)
	}
	if version != 1 {
		t.Fatalf("custom schema migration version=%d; want 1", version)
	}
}

func TestPgxMigrateUsesMaxVersionFromAppendOnlyHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	tableName := testTableName(t, "append_only_versions")
	sideEffectTable := testTableName(t, "append_only_side_effect")
	pool := pgxPool(ctx, t)
	dropTable(ctx, t, pool, tableName)
	dropTable(ctx, t, pool, sideEffectTable)
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+" (version bigint PRIMARY KEY)"); err != nil {
		t.Fatalf("create migration table %s: %v", tableName, err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+tableName+" (version) VALUES (1), (2)"); err != nil {
		t.Fatalf("seed migration table %s: %v", tableName, err)
	}
	t.Cleanup(func() {
		dropTable(ctx, t, pool, tableName)
		dropTable(ctx, t, pool, sideEffectTable)
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	migrator := New(Migrations{
		{
			Version: 2,
			Path:    "002-already-applied.sql",
			SQL:     "CREATE TABLE",
		},
		{
			Version: 3,
			Path:    "003-next.sql",
			SQL:     "CREATE TABLE " + sideEffectTable + " (id integer)",
		},
	}, newPgxDB(newPgxPoolConn(conn), tableName))

	if err := migrator.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}

	if !tableExists(ctx, t, pool, sideEffectTable) {
		t.Fatalf("side effect table %s does not exist; want next migration to run", sideEffectTable)
	}

	var count, maxVersion uint64
	if err := pool.QueryRow(ctx, "SELECT count(*), max(version) FROM "+tableName).Scan(&count, &maxVersion); err != nil {
		t.Fatalf("read migration history summary: %v", err)
	}
	if count != 3 || maxVersion != 3 {
		t.Fatalf("migration history count=%d max=%d; want count=3 max=3", count, maxVersion)
	}
}

func pgxPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(testDSN())
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect config: %v", err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func testDSN() string {
	if dsn := os.Getenv("MIG_TEST_DSN"); dsn != "" {
		return dsn
	}

	return dsn
}

func testTableName(t *testing.T, prefix string) string {
	t.Helper()

	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func dropTable(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tableName string) {
	t.Helper()

	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS "+tableName); err != nil {
		t.Fatalf("drop table %s: %v", tableName, err)
	}
}

func tableExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tableName string) bool {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", tableName).Scan(&exists); err != nil {
		t.Fatalf("check table %s exists: %v", tableName, err)
	}

	return exists
}

type lockIdentityConn struct {
	database string
	schema   string
}

func (conn lockIdentityConn) QueryRow(context.Context, string, ...any) pgx.Row {
	return lockIdentityRow(conn)
}

func (lockIdentityConn) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected Begin call")
}

type lockIdentityRow lockIdentityConn

func (row lockIdentityRow) Scan(dest ...any) error {
	*(dest[0].(*string)) = row.database
	*(dest[1].(*string)) = row.schema

	return nil
}

func expectedPgxLockID(database, schema, tableName string) string {
	name := strings.Join([]string{database, schema, tableName}, "\x00")
	sum := crc32.ChecksumIEEE([]byte(name))
	sum *= uint32(lockID)

	return strconv.FormatUint(uint64(sum), 10)
}
