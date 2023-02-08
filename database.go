package mig

import "context"

type Database interface {
	Lock(ctx context.Context) error
	CreateSchemaMigrationsTable(ctx context.Context) error
	LastVersion(ctx context.Context) (uint64, error)
	SetLastVersion(ctx context.Context, lastVersion uint64) error
	RunMigration(ctx context.Context, query string) error
	Unlock(ctx context.Context) error
}
