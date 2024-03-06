package mig

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"

	pgx4 "github.com/jackc/pgx/v4"
	pgxPool4 "github.com/jackc/pgx/v4/pgxpool"
	pgx5 "github.com/jackc/pgx/v5"
	pgxPool5 "github.com/jackc/pgx/v5/pgxpool"
)

type (
	pgxCmds interface {
		Exec(ctx context.Context, sql string, arguments ...any) (rowsAffected, error)
		QueryRow(ctx context.Context, sql string, args ...any) scan
		Begin(ctx context.Context) (tx, error)
	}

	rowsAffected interface{ RowsAffected() int64 }

	scan interface{ Scan(dest ...any) error }

	tx interface {
		Exec(ctx context.Context, sql string, arguments ...any) (rowsAffected, error)
		Commit(ctx context.Context) error
		Rollback(ctx context.Context) error
	}
)

type pgxDB struct {
	table  string
	lockID string
	conn   pgxCmds
}

func newPgxDB(conn pgxCmds, tableName string) *pgxDB {
	db := &pgxDB{ //nolint:exhaustruct
		table: tableName,
		conn:  conn,
	}

	return db
}

func (db *pgxDB) Lock(ctx context.Context) error {
	if err := db.setLockID(ctx); err != nil {
		return fmt.Errorf("set lock id: %w", err)
	}

	q := "SELECT pg_advisory_lock($1)"

	if _, err := db.conn.Exec(ctx, q, db.lockID); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *pgxDB) CreateSchemaMigrationsTable(ctx context.Context) error {
	q := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (version bigint PRIMARY KEY)", db.table)

	if _, err := db.conn.Exec(ctx, q); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *pgxDB) LastVersion(ctx context.Context) (uint64, error) {
	q := "SELECT version FROM " + db.table

	var version uint64

	if err := db.conn.QueryRow(ctx, q).Scan(&version); err != nil {
		if errors.Is(err, pgx4.ErrNoRows) || errors.Is(err, pgx5.ErrNoRows) {
			return 0, nil
		}

		return 0, fmt.Errorf("scan: %w", err)
	}

	return version, nil
}

func (db *pgxDB) SetLastVersion(ctx context.Context, lastVersion uint64) error {
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

func (db *pgxDB) RunMigration(ctx context.Context, query string) error {
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

func (db *pgxDB) Unlock(ctx context.Context) error {
	q := "SELECT pg_advisory_unlock($1)"

	if _, err := db.conn.Exec(ctx, q, db.lockID); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *pgxDB) setLockID(ctx context.Context) error {
	q := "SELECT CURRENT_DATABASE(), CURRENT_SCHEMA()"

	var database, schema string

	if err := db.conn.QueryRow(ctx, q).Scan(&database, &schema); err != nil {
		return fmt.Errorf("query row: %w", err)
	}

	name := strings.Join([]string{database, schema, db.table}, "\x00")
	sum := crc32.ChecksumIEEE([]byte(name))

	sum *= uint32(2854263694) //nolint:gomnd

	db.lockID = strconv.FormatUint(uint64(sum), 10)

	return nil
}

type pgx4conn struct {
	conn *pgx4.Conn
}

func (a *pgx4conn) Exec(ctx context.Context, sql string, args ...any) (rowsAffected, error) { //nolint:ireturn
	return a.conn.Exec(ctx, sql, args...) //nolint:wrapcheck
}

func (a *pgx4conn) QueryRow(ctx context.Context, sql string, args ...any) scan { //nolint:ireturn
	return a.conn.QueryRow(ctx, sql, args...)
}

func (a *pgx4conn) Begin(ctx context.Context) (tx, error) { //nolint:ireturn
	tx, err := a.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	return &tx4{tx}, nil
}

type pgx5conn struct {
	conn *pgx5.Conn
}

func (a *pgx5conn) Exec(ctx context.Context, sql string, args ...any) (rowsAffected, error) { //nolint:ireturn
	return a.conn.Exec(ctx, sql, args...) //nolint:wrapcheck
}

func (a *pgx5conn) QueryRow(ctx context.Context, sql string, args ...any) scan { //nolint:ireturn
	return a.conn.QueryRow(ctx, sql, args...)
}

func (a *pgx5conn) Begin(ctx context.Context) (tx, error) { //nolint:ireturn
	tx, err := a.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	return &tx5{tx}, nil
}

type pgx4pool struct {
	conn *pgxPool4.Conn
}

func (a *pgx4pool) Exec(ctx context.Context, sql string, args ...any) (rowsAffected, error) { //nolint:ireturn
	return a.conn.Exec(ctx, sql, args...) //nolint:wrapcheck
}

func (a *pgx4pool) QueryRow(ctx context.Context, sql string, args ...any) scan { //nolint:ireturn
	return a.conn.QueryRow(ctx, sql, args...)
}

func (a *pgx4pool) Begin(ctx context.Context) (tx, error) { //nolint:ireturn
	tx, err := a.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	return &tx4{tx}, nil
}

type tx4 struct {
	pgx4.Tx
}

func (a *tx4) Exec(ctx context.Context, sql string, args ...any) (rowsAffected, error) { //nolint:ireturn
	return a.Tx.Exec(ctx, sql, args...) //nolint:wrapcheck
}

type pgx5pool struct {
	conn *pgxPool5.Conn
}

func (a *pgx5pool) Exec(ctx context.Context, sql string, args ...any) (rowsAffected, error) { //nolint:ireturn
	return a.conn.Exec(ctx, sql, args...) //nolint:wrapcheck
}

func (a *pgx5pool) QueryRow(ctx context.Context, sql string, args ...any) scan { //nolint:ireturn
	return a.conn.QueryRow(ctx, sql, args...)
}

func (a *pgx5pool) Begin(ctx context.Context) (tx, error) { //nolint:ireturn
	tx, err := a.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	return &tx5{tx}, nil
}

type tx5 struct {
	pgx5.Tx
}

func (a *tx5) Exec(ctx context.Context, sql string, args ...any) (rowsAffected, error) { //nolint:ireturn
	return a.Tx.Exec(ctx, sql, args...) //nolint:wrapcheck
}
