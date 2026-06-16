package mig_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"go.acim.net/mig"
)

const dsn = "postgres://postgres@localhost:5432/mig"

func TestMigrate(t *testing.T) {
	t.Parallel()

	ms, err := mig.FromEmbedFS(ms, "migrations")
	if err != nil {
		t.Fatalf("from embed fs: %v", err)
	}

	db := &dbFake{} //nolint:exhaustruct

	m := mig.New(ms, db)

	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if db.v != 2 {
		t.Errorf("LastVersion()=%d; want %d", db.v, 2)
	}

	if db.l {
		t.Errorf("unlock not called")
	}
}

type dbFake struct {
	l bool
	v uint64
}

func (db *dbFake) Lock(context.Context) error {
	db.l = true

	return nil
}

func (db *dbFake) CreateSchemaMigrationsTable(context.Context) error {
	return nil
}

func (db *dbFake) LastVersion(context.Context) (uint64, error) {
	return db.v, nil
}

func (db *dbFake) SetLastVersion(_ context.Context, lastVersion uint64) error {
	db.v = lastVersion

	return nil
}

func (db *dbFake) RunMigration(context.Context, string) error {
	return nil
}

func (db *dbFake) Unlock(context.Context) error {
	db.l = false

	return nil
}

func ExampleFromPgxPool() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		panic(err)
	}

	defer pool.Close()

	migrator, cleanup, err := mig.FromPgxPool(migrations, pool, mig.WithCustomTable("eve"),
		mig.WithAcquireConnectionTimeout(time.Second))
	if err != nil {
		panic(err)
	}

	defer cleanup()

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}

func ExampleFromPgx() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		panic(err)
	}

	defer func() {
		if err := conn.Close(ctx); err != nil {
			panic(err)
		}
	}()

	migrator := mig.FromPgx(migrations, conn, mig.WithCustomTable("trudy"))

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}
