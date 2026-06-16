package mig

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dsn = "postgres://postgres@localhost:5432/mig"

var (
	_ pgxConn  = (*pgx.Conn)(nil)
	_ pgxConn  = (*pgxpool.Conn)(nil)
	_ Database = (*pgxDB)(nil)
)

func TestPgxPool(t *testing.T) { //nolint:cyclop
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	const tableName = "foo"

	ctx := context.Background()

	pool := pgxPool(ctx, t)

	q := "DROP TABLE IF EXISTS " + tableName

	if _, err := pool.Exec(ctx, q); err != nil {
		t.Fatalf("drop table before test: %v", err)
	}

	defer func() {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Errorf("drop table after test: %v", err)
		}
	}()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}

	defer conn.Release()

	db := newPgxDB(newPgxPoolConn(conn), tableName)

	if err := db.Lock(ctx); err != nil {
		t.Errorf("Lock(): %v", err)
	}

	if err := db.CreateSchemaMigrationsTable(ctx); err != nil {
		t.Errorf("CreateSchemaMigrationsTable(): %v", err)
	}

	var v uint64

	v, err = db.LastVersion(ctx)
	if err != nil {
		t.Errorf("LastVersion(): %v", err)
	}

	if v != 0 {
		t.Errorf("LastVersion()=%d; want %d", v, 0)
	}

	if err := db.SetLastVersion(ctx, 1); err != nil {
		t.Errorf("SetLastVersion(): %v", err)
	}

	q = "SELECT version FROM " + tableName

	if err := pool.QueryRow(ctx, q).Scan(&v); err != nil {
		t.Errorf("Scan(): %v", err)
	}

	if v != 1 {
		t.Errorf("SetLastVersion()=%d; want %d", v, 1)
	}

	if err := db.Unlock(ctx); err != nil {
		t.Errorf("Unlock(): %v", err)
	}
}

func TestPoolRunMigration(t *testing.T) {
	t.Parallel()

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

	db := newPgxDB(newPgxPoolConn(conn), "")

	if err := db.RunMigration(ctx, "CREATE TABLE baz (version serial); DROP TABLE baz"); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	if err := db.RunMigration(ctx, "CREATE TABLE"); err == nil {
		t.Fatal("run migration with broken query: want error; got no error")
	}
}

func pgxPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect config: %v", err)
	}

	return pool
}
