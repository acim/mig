package mig

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type Pgx4 struct {
	table  string
	lockID string
	conn   *pgxpool.Conn
}

func NewPgx4(conn *pgxpool.Conn, opts ...Pgx4Option) *Pgx4 {
	db := &Pgx4{ //nolint:exhaustruct
		table: "schema_migrations",
		conn:  conn,
	}

	for _, opt := range opts {
		opt(db)
	}

	return db
}

func (db *Pgx4) Lock(ctx context.Context) error {
	if err := db.setLockID(ctx); err != nil {
		return fmt.Errorf("set lock id: %w", err)
	}

	q := "SELECT pg_advisory_lock($1)"

	if _, err := db.conn.Exec(ctx, q, db.lockID); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *Pgx4) CreateSchemaMigrationsTable(ctx context.Context) error {
	q := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (version bigint PRIMARY KEY)", db.table)

	if _, err := db.conn.Exec(ctx, q); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *Pgx4) LastVersion(ctx context.Context) (uint64, error) {
	q := fmt.Sprintf("SELECT version FROM %s", db.table)

	var version uint64

	if err := db.conn.QueryRow(ctx, q).Scan(&version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}

		return 0, fmt.Errorf("scan: %w", err)
	}

	return version, nil
}

func (db *Pgx4) SetLastVersion(ctx context.Context, lastVersion uint64) error {
	q := fmt.Sprintf("UPDATE %s SET version=$1", db.table)

	ct, err := db.conn.Exec(ctx, q, lastVersion)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if ct.RowsAffected() == 1 {
		return nil
	}

	q = fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", db.table)

	if _, err := db.conn.Exec(ctx, q, lastVersion); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *Pgx4) RunMigration(ctx context.Context, query string) error {
	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	if _, err := tx.Exec(ctx, query); err != nil {
		if err := tx.Rollback(ctx); err != nil {
			return fmt.Errorf("rollback: %w", err)
		}

		return fmt.Errorf("exec: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func (db *Pgx4) Unlock(ctx context.Context) error {
	q := "SELECT pg_advisory_unlock($1)"

	if _, err := db.conn.Exec(ctx, q, db.lockID); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	db.conn.Release()

	return nil
}

func (db *Pgx4) setLockID(ctx context.Context) error {
	q := "SELECT CURRENT_DATABASE(), CURRENT_SCHEMA()"

	var database, schema string

	if err := db.conn.QueryRow(ctx, q).Scan(&database, &schema); err != nil {
		return fmt.Errorf("query row: %w", err)
	}

	name := strings.Join([]string{database, schema, db.table}, "\x00")
	sum := crc32.ChecksumIEEE([]byte(name))

	sum *= uint32(2854263694) //nolint:gomnd

	db.lockID = fmt.Sprint(sum)

	return nil
}

type Pgx4Option func(*Pgx4)

func WithCustomTable(name string) Pgx4Option {
	return func(db *Pgx4) {
		db.table = name
	}
}
