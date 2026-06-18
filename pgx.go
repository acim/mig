package mig

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const lockID = 2854263694

type pgxConn interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

type pgxExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type pgxDB struct {
	table  string
	lockID string
	conn   pgxConn
}

func newPgxDB(conn pgxConn, tableName string) *pgxDB {
	db := &pgxDB{ //nolint:exhaustruct
		table: sanitizeTableName(tableName),
		conn:  conn,
	}

	return db
}

func sanitizeTableName(tableName string) string {
	return pgx.Identifier(strings.Split(tableName, ".")).Sanitize()
}

func (db *pgxDB) createSchemaMigrationsTable(ctx context.Context, exec pgxExecutor) error {
	q := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (version bigint PRIMARY KEY)", db.table)

	if _, err := exec.Exec(ctx, q); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *pgxDB) lastVersion(ctx context.Context, exec pgxExecutor) (uint64, error) {
	q := "SELECT COALESCE(max(version), 0) FROM " + db.table

	var version uint64

	if err := exec.QueryRow(ctx, q).Scan(&version); err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	return version, nil
}

func (db *pgxDB) setLastVersion(ctx context.Context, exec pgxExecutor, lastVersion uint64) error {
	q := fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", db.table)

	if _, err := exec.Exec(ctx, q, lastVersion); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *pgxDB) Migrate(ctx context.Context, ms Migrations) (err error) {
	if err := db.setLockID(ctx); err != nil {
		return fmt.Errorf("set lock id: %w", err)
	}

	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}

	done := false
	defer func() {
		if done {
			return
		}

		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback migration transaction: %w", rollbackErr))
		}
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", db.lockID); err != nil {
		return fmt.Errorf("lock migration transaction: %w", err)
	}

	if err := db.createSchemaMigrationsTable(ctx, tx); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	lastVersion, err := db.lastVersion(ctx, tx)
	if err != nil {
		return fmt.Errorf("last version: %w", err)
	}

	for _, m := range ms {
		if m.Version <= lastVersion {
			continue
		}

		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("run migration %d from file %s: execute migration SQL: %w", m.Version, m.Path, err)
		}

		if err := db.setLastVersion(ctx, tx, m.Version); err != nil {
			return fmt.Errorf("set last version %d: %w", m.Version, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		done = true
		return fmt.Errorf("commit migration transaction: %w", err)
	}

	done = true

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

	sum *= uint32(lockID)

	db.lockID = strconv.FormatUint(uint64(sum), 10)

	return nil
}

func newPgxConn(conn *pgx.Conn) pgxConn {
	return conn
}

func newPgxPoolConn(conn *pgxpool.Conn) pgxConn {
	return conn
}
