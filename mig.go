package mig

import (
	"context"
	"fmt"
	"time"

	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Database interface {
	Migrate(ctx context.Context, ms Migrations) error
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

func FromPgxPool(ms Migrations, pool *pgxpool.Pool, opts ...Option) (*Mig, func(), error) {
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

	m.db = newPgxDB(newPgxPoolConn(conn), m.table)

	return m, conn.Release, nil
}

func FromPgx(ms Migrations, conn *pgx.Conn, opts ...Option) *Mig {
	m := New(ms, nil, opts...)

	m.db = newPgxDB(newPgxConn(conn), m.table)

	return m
}

func (d *Mig) Migrate(ctx context.Context) error {
	if err := d.ms.Validate(); err != nil {
		return err
	}

	return d.db.Migrate(ctx, d.ms)
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
