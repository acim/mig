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
	pool := pgxPool(ctx, t)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	migrator := New(nil, newPgxDB(newPgxPoolConn(conn), "broken table name"))

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
