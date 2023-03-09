package mig

import (
	"context"
	"fmt"
	"testing"

	pgxPoolV4 "github.com/jackc/pgx/v4/pgxpool"
	pgxPoolV5 "github.com/jackc/pgx/v5/pgxpool"
)

const dsn = "postgres://postgres@localhost:5432/mig"

var (
	_ pgxCmds  = (*pgx4pool)(nil)
	_ pgxCmds  = (*pgx5pool)(nil)
	_ pgxCmds  = (*pgx4conn)(nil)
	_ Database = (*pgxDB)(nil)
)

func TestPgxPool(t *testing.T) { //nolint:cyclop
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	const tableName = "foo"

	ctx := context.Background()

	pool := pgx4Pool(ctx, t)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}

	defer conn.Release()

	db := newPgxDB(&pgx4pool{
		conn: conn,
	}, tableName)

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

	q := fmt.Sprintf("SELECT version FROM %s", tableName)

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

func TestPoolV4RunMigration(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()

	pool := pgx4Pool(ctx, t)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}

	defer conn.Release()

	db := newPgxDB(&pgx4pool{
		conn: conn,
	}, "")

	if err := db.RunMigration(ctx, "CREATE TABLE bar (version serial); DROP TABLE bar"); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	if err := db.RunMigration(ctx, "CREATE TABLE"); err == nil {
		t.Fatal("run migration with broken query: want error; got no error")
	}
}

func TestPoolV5RunMigration(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()

	pool := pgx5Pool(ctx, t)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}

	defer conn.Release()

	db := newPgxDB(&pgx5pool{
		conn: conn,
	}, "")

	if err := db.RunMigration(ctx, "CREATE TABLE baz (version serial); DROP TABLE baz"); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	if err := db.RunMigration(ctx, "CREATE TABLE"); err == nil {
		t.Fatal("run migration with broken query: want error; got no error")
	}
}

func pgx4Pool(ctx context.Context, t *testing.T) *pgxPoolV4.Pool {
	t.Helper()

	cfg, err := pgxPoolV4.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	pool, err := pgxPoolV4.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect config: %v", err)
	}

	return pool
}

func pgx5Pool(ctx context.Context, t *testing.T) *pgxPoolV5.Pool {
	t.Helper()

	cfg, err := pgxPoolV5.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	pool, err := pgxPoolV5.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect config: %v", err)
	}

	return pool
}
