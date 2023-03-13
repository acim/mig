package mig

import (
	"context"
	"errors"
	"fmt"
	"time"

	pgx4 "github.com/jackc/pgx/v4"
	pgxPoolV4 "github.com/jackc/pgx/v4/pgxpool"
	pgx5 "github.com/jackc/pgx/v5"
	pgxPoolV5 "github.com/jackc/pgx/v5/pgxpool"
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
	timeout time.Duration
	ms      Migrations
	db      Database
	table   string
}

func New(ms Migrations, db Database, opts ...Option) *Mig {
	m := &Mig{ //nolint:exhaustruct
		ms:    ms,
		db:    db,
		table: "schema_migrations",
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func FromPgxV4Pool(ms Migrations, pool *pgxPoolV4.Pool, opts ...Option) (*Mig, func(), error) {
	m := New(ms, nil, opts...)

	ctx := context.Background()
	cancel := func() {}

	if m.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
	}

	defer cancel()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire connection: %w", err)
	}

	m.db = newPgxDB(&pgx4pool{
		conn: conn,
	}, m.table)

	return m, conn.Release, nil
}

func FromPgxV5Pool(ms Migrations, pool *pgxPoolV5.Pool, opts ...Option) (*Mig, func(), error) {
	m := New(ms, nil, opts...)

	ctx := context.Background()
	cancel := func() {}

	if m.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
	}

	defer cancel()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire connection: %w", err)
	}

	m.db = newPgxDB(&pgx5pool{
		conn: conn,
	}, m.table)

	return m, conn.Release, nil
}

func FromPgxV4(ms Migrations, conn *pgx4.Conn, opts ...Option) *Mig {
	m := New(ms, nil, opts...)

	m.db = newPgxDB(&pgx4conn{
		conn: conn,
	}, m.table)

	return m
}

func FromPgxV5(ms Migrations, conn *pgx5.Conn, opts ...Option) *Mig {
	m := New(ms, nil, opts...)

	m.db = newPgxDB(&pgx5conn{
		conn: conn,
	}, m.table)

	return m
}

func (d *Mig) Migrate(ctx context.Context) error {
	err := d.db.Lock(ctx)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}

	defer func() {
		err = errors.Join(err, d.db.Unlock(ctx))
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
				return fmt.Errorf("run migration %d from file %s: %w", m.Version, m.Path, err)
			}

			if err := d.db.SetLastVersion(ctx, m.Version); err != nil {
				return fmt.Errorf("set last version %d: %w", m.Version, err)
			}
		}
	}

	return nil
}

type Option func(*Mig)

func WithCustomTable(name string) Option {
	return func(m *Mig) {
		m.table = name
	}
}

func WithAcquireConnectionTimeout(timeout time.Duration) Option {
	return func(m *Mig) {
		m.timeout = timeout
	}
}
