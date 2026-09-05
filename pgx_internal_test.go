package mig

import (
	"context"
	"errors"
	"fmt"
	"os"
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

func TestPgxLockIDUsesCanonicalLedgerIdentity(t *testing.T) {
	t.Parallel()

	unqualified := newPgxDB(lockIdentityConn{database: "mig", schema: "app"}, "schema_migrations")
	qualified := newPgxDB(lockIdentityConn{database: "mig", schema: "public"}, "app.schema_migrations")

	for _, db := range []*pgxDB{unqualified, qualified} {
		if err := db.setLockID(context.Background()); err != nil {
			t.Fatalf("setLockID(): %v", err)
		}
	}

	if unqualified.table != qualified.table {
		t.Fatalf("canonical tables differ: unqualified=%s qualified=%s", unqualified.table, qualified.table)
	}
	if unqualified.lockID != qualified.lockID {
		t.Fatalf("lock IDs differ for the same ledger: unqualified=%v qualified=%v", unqualified.lockID, qualified.lockID)
	}
}

func TestPgxMigrateSerializesQualifiedAndUnqualifiedLedgerAliases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	schemaName := testTableName(t, "ledger_schema")
	ledgerName := "schema_migrations"
	sideEffectTable := schemaName + ".migration_side_effect"
	pool := pgxPool(ctx, t)
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); err != nil {
		t.Fatalf("drop schema before test: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); err != nil {
			t.Errorf("drop schema after test: %v", err)
		}
	})

	unqualifiedConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire unqualified connection: %v", err)
	}
	defer unqualifiedConn.Release()
	if _, err := unqualifiedConn.Exec(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set unqualified search path: %v", err)
	}

	qualifiedConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire qualified connection: %v", err)
	}
	defer qualifiedConn.Release()
	if _, err := qualifiedConn.Exec(ctx, "SET search_path TO public, "+schemaName); err != nil {
		t.Fatalf("set qualified search path: %v", err)
	}

	migrations := Migrations{{
		Version: 1,
		Path:    "001-concurrent.sql",
		SQL:     "SELECT pg_sleep(0.2); CREATE TABLE " + sideEffectTable + " (id integer)",
	}}
	migrators := []*Mig{
		New(migrations, newPgxDB(newPgxPoolConn(unqualifiedConn), ledgerName)),
		New(migrations, newPgxDB(newPgxPoolConn(qualifiedConn), schemaName+"."+ledgerName)),
	}

	start := make(chan struct{})
	errs := make(chan error, len(migrators))
	for _, migrator := range migrators {
		go func() {
			<-start
			errs <- migrator.Migrate(ctx)
		}()
	}
	close(start)

	for range migrators {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Migrate(): %v", err)
		}
	}

	var versionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+schemaName+"."+ledgerName).Scan(&versionCount); err != nil {
		t.Fatalf("count ledger versions: %v", err)
	}
	if versionCount != 1 {
		t.Fatalf("ledger version count=%d; want 1", versionCount)
	}
}

func TestPgxMigrateWrapsBeginError(t *testing.T) {
	t.Parallel()

	db := newPgxDB(lockIdentityConn{database: "mig", schema: "public"}, "schema_migrations")

	err := db.Migrate(context.Background(), nil)
	if err == nil {
		t.Fatal("Migrate() error=<nil>; want begin error")
	}

	if !strings.Contains(err.Error(), "begin migration transaction") {
		t.Fatalf("Migrate() error=%q; want begin transaction context", err)
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

func TestPgxMigrateRejectsTransactionControlStatementsWithoutRecordingVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	tests := []struct {
		name string
		sql  string
	}{
		{name: "rollback", sql: "ROLLBACK"},
		{name: "commit", sql: "COMMIT"},
		{name: "end", sql: "END TRANSACTION"},
		{name: "abort", sql: "ABORT"},
		{name: "begin", sql: "BEGIN"},
		{name: "start transaction", sql: "START TRANSACTION"},
		{name: "savepoint", sql: "SAVEPOINT nested"},
		{name: "release savepoint", sql: "RELEASE SAVEPOINT nested"},
		{name: "prepare transaction", sql: "PREPARE TRANSACTION 'migration'"},
		{name: "set transaction", sql: "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"},
		{name: "after another statement", sql: "SELECT 1; ROLLBACK"},
		{name: "after comments", sql: "-- migration setup\n/* nested /* comment */ comment */ COMMIT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tableName := testTableName(t, "transaction_control")
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

			migrator := New(Migrations{{
				Version: 1,
				Path:    "001-transaction-control.sql",
				SQL:     tt.sql,
			}}, newPgxDB(newPgxPoolConn(conn), tableName))

			err = migrator.Migrate(ctx)
			if !errors.Is(err, ErrTransactionControl) {
				t.Fatalf("Migrate() error=%v; want transaction control error", err)
			}

			if tableExists(ctx, t, pool, tableName) {
				var count int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+tableName).Scan(&count); err != nil {
					t.Fatalf("count migration versions: %v", err)
				}
				if count != 0 {
					t.Fatalf("migration version count=%d; want 0", count)
				}
			}
		})
	}
}

func TestPgxMigrateAllowsTransactionKeywordsInCommentsAndQuotedText(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()
	tableName := testTableName(t, "quoted_transaction_words")
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

	migrator := New(Migrations{{
		Version: 1,
		Path:    "001-quoted-transaction-words.sql",
		SQL: `
			-- ROLLBACK;
			/* COMMIT; */
			SELECT 'BEGIN; END', $$ABORT; START TRANSACTION$$;
		`,
	}}, newPgxDB(newPgxPoolConn(conn), tableName))

	if err := migrator.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}

	var version uint64
	if err := pool.QueryRow(ctx, "SELECT version FROM "+tableName).Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != 1 {
		t.Fatalf("migration version=%d; want 1", version)
	}
}

func TestContainsTransactionControlDistinguishesTopLevelCommandsFromQuotedText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "set session transaction", sql: "SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY", want: true},
		{name: "lowercase rollback", sql: "select 1; rollback", want: true},
		{name: "parameter before command", sql: "SELECT $1; COMMIT", want: true},
		{name: "prepared statement", sql: "PREPARE query AS SELECT 1", want: false},
		{name: "start expression", sql: "SELECT 'START TRANSACTION'", want: false},
		{name: "escaped quote", sql: `SELECT E'ROLLBACK\'; COMMIT'`, want: false},
		{name: "backslash in standard string", sql: `SELECT 'ROLLBACK\'; COMMIT`, want: true},
		{name: "doubled single quote", sql: "SELECT 'COMMIT''ROLLBACK'", want: false},
		{name: "doubled identifier quote", sql: `SELECT "COMMIT""ROLLBACK"`, want: false},
		{name: "tagged dollar quote", sql: "DO $body$ BEGIN RAISE NOTICE 'COMMIT'; END $body$", want: false},
		{name: "unterminated dollar quote", sql: "SELECT $body$ ROLLBACK", want: false},
		{name: "unterminated quoted text", sql: "SELECT 'ROLLBACK", want: false},
		{name: "empty SQL", sql: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := containsTransactionControl(tt.sql); got != tt.want {
				t.Fatalf("containsTransactionControl(%q)=%t; want %t", tt.sql, got, tt.want)
			}
		})
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

func TestLockIdentityRowScanRejectsInvalidDestinations(t *testing.T) {
	t.Parallel()

	row := lockIdentityRow{}

	if err := row.Scan(new(string)); err == nil {
		t.Fatal("expected an error for the wrong destination count")
	}
	if err := row.Scan(new(int), new(string)); err == nil {
		t.Fatal("expected an error for an invalid database destination")
	}
	if err := row.Scan(new(string), new(int)); err == nil {
		t.Fatal("expected an error for an invalid schema destination")
	}
}

func (row lockIdentityRow) Scan(dest ...any) error {
	if len(dest) != 2 {
		return fmt.Errorf("scan lock identity: got %d destinations, want 2", len(dest))
	}

	database, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("scan lock identity database into %T: want *string", dest[0])
	}
	schema, ok := dest[1].(*string)
	if !ok {
		return fmt.Errorf("scan lock identity schema into %T: want *string", dest[1])
	}

	*database = row.database
	*schema = row.schema

	return nil
}
