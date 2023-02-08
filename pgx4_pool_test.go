package mig_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v4/pgxpool"
	"go.acim.net/mig"
)

const dsn = "postgres://postgres@postgres:5432/mig"

func TestCreateSchemaMigrationsTable(t *testing.T) {
	ctx := context.Background()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}

	db := mig.NewPgx4(conn)

	if err := db.Lock(ctx); err != nil {
		t.Errorf("Lock(): %v", err)
	}

	if err := db.CreateSchemaMigrationsTable(context.Background()); err != nil {
		t.Errorf("CreateSchemaMigrationsTable(): %v", err)
	}

	v, err := db.LastVersion(ctx)
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

	var version uint64

	if err := conn.QueryRow(ctx, q).Scan(&version); err != nil {
		t.Errorf("Scan(): %v", err)
	}

	if version != 1 {
		t.Errorf("SetLastVersion()=%d; want %d", version, 1)
	}

	if err := db.Unlock(ctx); err != nil {
		t.Errorf("Unlock(): %v", err)
	}
}
