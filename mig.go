package mig

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Database interface {
	Migrate(ctx context.Context, ms Migrations) error
}

var ErrInvalidTableName = errors.New("invalid table name")

var tableNamePartPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Mig struct {
	timeout time.Duration
	ms      Migrations
	db      Database
	table   string
	err     error
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
	if m.err != nil {
		return nil, nil, m.err
	}

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
	if d.err != nil {
		return d.err
	}

	if err := d.ms.Validate(); err != nil {
		return err
	}

	return d.db.Migrate(ctx, d.ms)
}

type Option func(*Mig)

func WithCustomTable(name string) Option {
	return func(m *Mig) {
		if err := validateTableName(name); err != nil {
			m.err = err
			return
		}

		m.table = name
	}
}

func WithAcquireConnectionTimeout(timeout time.Duration) Option {
	return func(m *Mig) {
		m.timeout = timeout
	}
}

func validateTableName(name string) error {
	parts := strings.Split(name, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return fmt.Errorf("%w: %s", ErrInvalidTableName, name)
	}

	for _, part := range parts {
		if !tableNamePartPattern.MatchString(part) {
			return fmt.Errorf("%w: %s", ErrInvalidTableName, name)
		}
	}

	return nil
}
