package mig

import (
	"context"
	"fmt"
)

type Database interface {
	Lock(ctx context.Context) error
	CreateSchemaMigrationsTable(ctx context.Context) error
	LastVersion(ctx context.Context) (uint64, error)
	SetLastVersion(ctx context.Context, lastVersion uint64) error
	RunMigration(ctx context.Context, query string) error
	Unlock(ctx context.Context) error
}

type Mig struct {
	ms Migrations
	db Database
}

func NewMig(ms Migrations, db Database) *Mig {
	return &Mig{
		ms: ms,
		db: db,
	}
}

func (d *Mig) Migrate(ctx context.Context) error {
	if err := d.db.Lock(ctx); err != nil {
		return fmt.Errorf("lock: %w", err)
	}

	defer func() {
		d.db.Unlock(ctx) //nolint:errcheck
	}()

	if err := d.db.CreateSchemaMigrationsTable(ctx); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	lastVersion, err := d.db.LastVersion(ctx)
	if err != nil {
		return fmt.Errorf("last version: %w", err)
	}

	for _, m := range d.ms {
		if m.Version > lastVersion {
			if err := d.db.RunMigration(ctx, m.SQL); err != nil {
				return fmt.Errorf("run migration %d: %w", m.Version, err)
			}

			if err := d.db.SetLastVersion(ctx, m.Version); err != nil {
				return fmt.Errorf("set last version %d: %w", m.Version, err)
			}
		}
	}

	return nil
}
