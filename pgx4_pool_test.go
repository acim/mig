package mig_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v4/pgxpool"
	"go.acim.net/mig"
)

const dsn = "postgres://postgres@localhost:5432/mig"

func TestPgxV4(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()

	conn, cleanup := pool(ctx, t)

	defer cleanup()

	db := mig.NewPgx4(conn)

	if err := db.Lock(ctx); err != nil {
		t.Errorf("Lock(): %v", err)
	}

	if err := db.CreateSchemaMigrationsTable(ctx); err != nil {
		t.Errorf("CreateSchemaMigrationsTable(): %v", err)
	}

	var (
		v   uint64
		err error
	)

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

	q := "SELECT version FROM schema_migrations"

	if err := conn.QueryRow(ctx, q).Scan(&v); err != nil {
		t.Errorf("Scan(): %v", err)
	}

	if v != 1 {
		t.Errorf("SetLastVersion()=%d; want %d", v, 1)
	}

	if err := db.Unlock(ctx); err != nil {
		t.Errorf("Unlock(): %v", err)
	}
}

func TestRunMigration(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()

	conn, cleanup := pool(ctx, t)

	defer cleanup()

	db := mig.NewPgx4(conn, mig.WithCustomTable("mig"))

	if err := db.RunMigration(ctx, "CREATE TABLE dummy (version serial); DROP TABLE dummy"); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	if err := db.RunMigration(ctx, "CREATE TABLE"); err == nil {
		t.Fatal("run migration with broken query: want error; got no error")
	}
}

func pool(ctx context.Context, t *testing.T) (*pgxpool.Conn, func()) {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	pool, err := pgxpool.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect config: %v", err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	return conn, func() {
		conn.Release()
		pool.Close()
	}
}
