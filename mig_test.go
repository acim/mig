package mig_test

import (
	"context"
	"testing"

	"go.acim.net/mig"
)

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
