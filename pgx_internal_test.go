package mig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dsn = "postgres://postgres@localhost:5432/mig"

var (
	_ pgxConn  = (*pgx.Conn)(nil)
	_ pgxConn  = (*pgxpool.Conn)(nil)
	_ Database = (*pgxDB)(nil)
)

func TestPgxPool(t *testing.T) { //nolint:cyclop
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	const tableName = "foo"

	ctx := context.Background()

	pool := pgxPool(ctx, t)

	q := "DROP TABLE IF EXISTS " + tableName

	if _, err := pool.Exec(ctx, q); err != nil {
		t.Fatalf("drop table before test: %v", err)
	}

	defer func() {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Errorf("drop table after test: %v", err)
		}
	}()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}

	defer conn.Release()

	db := newPgxDB(newPgxPoolConn(conn), tableName)

	if err := db.Lock(ctx); err != nil {
		t.Errorf("Lock(): %v", err)
	}

	if err := db.CreateSchemaMigrationsTable(ctx); err != nil {
		t.Errorf("CreateSchemaMigrationsTable(): %v", err)
	}

	var v uint64

	v, err = db.LastVersion(ctx)
	if err != nil {
		t.Errorf("LastVersion(): %v", err)
	}

	if v != 0 {
		t.Errorf("LastVersion()=%d; want %d", v, 0)
	}

	if err := db.SetLastVersion(ctx, 1); err != nil {
		t.Errorf("SetLastVersion(): %v", err)
	}

	q = "SELECT version FROM " + tableName

	if err := pool.QueryRow(ctx, q).Scan(&v); err != nil {
		t.Errorf("Scan(): %v", err)
	}

	if v != 1 {
		t.Errorf("SetLastVersion()=%d; want %d", v, 1)
	}

	if err := db.Unlock(ctx); err != nil {
		t.Errorf("Unlock(): %v", err)
	}
}

func TestPoolRunMigration(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping long test")
	}

	ctx := context.Background()

	pool := pgxPool(ctx, t)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}

	defer conn.Release()

	db := newPgxDB(newPgxPoolConn(conn), "")

	if err := db.RunMigration(ctx, "CREATE TABLE baz (version serial); DROP TABLE baz"); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	if err := db.RunMigration(ctx, "CREATE TABLE"); err == nil {
		t.Fatal("run migration with broken query: want error; got no error")
	}
}

func TestRunMigrationWrapsExecError(t *testing.T) {
	t.Parallel()

	execErr := errors.New("syntax failed")
	db := newPgxDB(connFake{tx: &txFake{execErr: execErr}}, "")

	err := db.RunMigration(context.Background(), "broken")
	if !errors.Is(err, execErr) {
		t.Fatalf("RunMigration() error=%v; want exec error", err)
	}

	if !strings.Contains(err.Error(), "execute migration SQL: syntax failed") {
		t.Fatalf("RunMigration() error=%q; want execute migration SQL context", err)
	}
}

func TestRunMigrationJoinsExecAndRollbackErrors(t *testing.T) {
	t.Parallel()

	execErr := errors.New("syntax failed")
	rollbackErr := errors.New("rollback failed")
	db := newPgxDB(connFake{tx: &txFake{
		execErr:     execErr,
		rollbackErr: rollbackErr,
	}}, "")

	err := db.RunMigration(context.Background(), "broken")
	if !errors.Is(err, execErr) {
		t.Fatalf("RunMigration() error=%v; want exec error", err)
	}

	if !errors.Is(err, rollbackErr) {
		t.Fatalf("RunMigration() error=%v; want rollback error", err)
	}

	for _, want := range []string{
		"execute migration SQL: syntax failed",
		"rollback migration transaction: rollback failed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("RunMigration() error=%q; want %q", err, want)
		}
	}
}

type txFake struct {
	execErr     error
	rollbackErr error
	commitErr   error
}

func (tx *txFake) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested transaction")
}

func (tx *txFake) Commit(context.Context) error {
	return tx.commitErr
}

func (tx *txFake) Rollback(context.Context) error {
	return tx.rollbackErr
}

func (tx *txFake) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected copy from")
}

func (tx *txFake) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (tx *txFake) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (tx *txFake) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected prepare")
}

func (tx *txFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.execErr
}

func (tx *txFake) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (tx *txFake) QueryRow(context.Context, string, ...any) pgx.Row {
	return rowFake{}
}

func (tx *txFake) Conn() *pgx.Conn {
	return nil
}

type connFake struct {
	tx       *txFake
	beginErr error
}

func (conn connFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

func (conn connFake) QueryRow(context.Context, string, ...any) pgx.Row {
	return rowFake{}
}

func (conn connFake) Begin(context.Context) (pgx.Tx, error) {
	if conn.beginErr != nil {
		return nil, conn.beginErr
	}

	return conn.tx, nil
}

type rowFake struct{}

func (rowFake) Scan(...any) error {
	return errors.New("unexpected scan")
}

func pgxPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect config: %v", err)
	}

	t.Cleanup(pool.Close)

	return pool
}
