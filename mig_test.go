package mig_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pgx4 "github.com/jackc/pgx/v4"
	pgx4pool "github.com/jackc/pgx/v4/pgxpool"
	pgx5 "github.com/jackc/pgx/v5"
	pgx5pool "github.com/jackc/pgx/v5/pgxpool"
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

func ExampleFromPgxV4Pool() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	pool, err := pgx4pool.Connect(ctx, dsn)
	if err != nil {
		panic(err)
	}

	migrator, cleanup, err := mig.FromPgxV4Pool(migrations, pool, mig.WithCustomTable("alice"))
	if err != nil {
		panic(err)
	}

	defer cleanup()

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}

func ExampleFromPgxV4() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	conn, err := pgx4.Connect(ctx, dsn)
	if err != nil {
		panic(err)
	}

	migrator := mig.FromPgxV4(migrations, conn, mig.WithCustomTable("bob"))

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}

func ExampleFromPgxV5Pool() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	pool, err := pgx5pool.New(ctx, dsn)
	if err != nil {
		panic(err)
	}

	migrator, cleanup, err := mig.FromPgxV5Pool(migrations, pool, mig.WithCustomTable("eve"))
	if err != nil {
		panic(err)
	}

	defer cleanup()

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}

func ExampleFromPgxV5() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	conn, err := pgx5.Connect(ctx, dsn)
	if err != nil {
		panic(err)
	}

	migrator := mig.FromPgxV5(migrations, conn, mig.WithCustomTable("trudy"))

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}
