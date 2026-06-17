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
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

type pgxDB struct {
	table  string
	lockID string
	conn   pgxConn
}

func newPgxDB(conn pgxConn, tableName string) *pgxDB {
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
		if errors.Is(err, pgx.ErrNoRows) {
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
		return fmt.Errorf("begin migration transaction: %w", err)
	}

	if _, err := tx.Exec(ctx, query); err != nil {
		err = fmt.Errorf("execute migration SQL: %w", err)
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback migration transaction: %w", rollbackErr))
		}

		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
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
