package mig_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"go.acim.net/mig"
)

const dsn = "postgres://postgres@localhost:5432/mig"

func testDSN() string {
	if dsn := os.Getenv("MIG_TEST_DSN"); dsn != "" {
		return dsn
	}

	return dsn
}

func TestMigrate(t *testing.T) {
	t.Parallel()

	ms, err := mig.FromEmbedFS(ms, "migrations")
	if err != nil {
		t.Fatalf("from embed fs: %v", err)
	}

	db := &dbFake{} //nolint:exhaustruct

	m := mig.New(ms, db)

	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if db.v != 2 {
		t.Errorf("LastVersion()=%d; want %d", db.v, 2)
	}

	if db.l {
		t.Errorf("unlock not called")
	}
}

type dbFake struct {
	l             bool
	v             uint64
	migrateCalled bool

	lockErr         error
	createTableErr  error
	lastVersionErr  error
	runMigrationErr error
	setVersionErr   error
	unlockErr       error
}

func (db *dbFake) Migrate(ctx context.Context, ms mig.Migrations) (err error) {
	db.migrateCalled = true

	err = db.Lock(ctx)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}

	defer func() {
		if unlockErr := db.Unlock(ctx); unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("unlock: %w", unlockErr))
		}
	}()

	if err := db.CreateSchemaMigrationsTable(ctx); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	lastVersion, err := db.LastVersion(ctx)
	if err != nil {
		return fmt.Errorf("last version: %w", err)
	}

	for _, m := range ms {
		if m.Version > lastVersion {
			if err := db.RunMigration(ctx, m.SQL); err != nil {
				return fmt.Errorf("run migration %d from file %s: %w", m.Version, m.Path, err)
			}

			if err := db.SetLastVersion(ctx, m.Version); err != nil {
				return fmt.Errorf("set last version %d: %w", m.Version, err)
			}
		}
	}

	return nil
}

func TestMigrateReturnsInvalidTableNameError(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"",
		"bad name",
		"schema_migrations; DROP TABLE users",
		"one.two.three",
		"1schema_migrations",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := &dbFake{} //nolint:exhaustruct
			m := mig.New(mig.Migrations{}, db, mig.WithCustomTable(name))

			err := m.Migrate(context.Background())
			if !errors.Is(err, mig.ErrInvalidTableName) {
				t.Fatalf("Migrate() error=%v; want invalid table name error", err)
			}

			if db.migrateCalled {
				t.Fatal("database Migrate called for invalid table name")
			}
		})
	}
}

func (db *dbFake) Lock(context.Context) error {
	db.l = true

	return db.lockErr
}

func (db *dbFake) CreateSchemaMigrationsTable(context.Context) error {
	return db.createTableErr
}

func (db *dbFake) LastVersion(context.Context) (uint64, error) {
	return db.v, db.lastVersionErr
}

func (db *dbFake) SetLastVersion(_ context.Context, lastVersion uint64) error {
	if db.setVersionErr != nil {
		return db.setVersionErr
	}

	db.v = lastVersion

	return nil
}

func (db *dbFake) RunMigration(context.Context, string) error {
	return db.runMigrationErr
}

func (db *dbFake) Unlock(context.Context) error {
	db.l = false

	return db.unlockErr
}

func TestMigrateReturnsUnlockError(t *testing.T) {
	t.Parallel()

	unlockErr := errors.New("unlock failed")
	db := &dbFake{unlockErr: unlockErr} //nolint:exhaustruct
	m := mig.New(mig.Migrations{}, db)

	err := m.Migrate(context.Background())
	if !errors.Is(err, unlockErr) {
		t.Fatalf("Migrate() error=%v; want unlock error", err)
	}

	if !strings.Contains(err.Error(), "unlock: unlock failed") {
		t.Fatalf("Migrate() error=%q; want unlock context", err)
	}
}

func TestMigrateJoinsMigrationAndUnlockErrors(t *testing.T) {
	t.Parallel()

	runErr := errors.New("migration failed")
	unlockErr := errors.New("unlock failed")
	db := &dbFake{
		runMigrationErr: runErr,
		unlockErr:       unlockErr,
	} //nolint:exhaustruct
	m := mig.New(mig.Migrations{{
		Version: 7,
		Path:    "007-broken.sql",
		SQL:     "broken",
	}}, db)

	err := m.Migrate(context.Background())
	if !errors.Is(err, runErr) {
		t.Fatalf("Migrate() error=%v; want migration error", err)
	}

	if !errors.Is(err, unlockErr) {
		t.Fatalf("Migrate() error=%v; want unlock error", err)
	}

	for _, want := range []string{
		"run migration 7 from file 007-broken.sql: migration failed",
		"unlock: unlock failed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Migrate() error=%q; want %q", err, want)
		}
	}
}

func TestMigrateWrapsSetupErrors(t *testing.T) {
	t.Parallel()

	lockErr := errors.New("lock failed")
	createErr := errors.New("create failed")
	lastVersionErr := errors.New("last version failed")

	tests := []struct {
		name string
		db   *dbFake
		want string
		err  error
	}{
		{
			name: "lock",
			db:   &dbFake{lockErr: lockErr}, //nolint:exhaustruct
			want: "lock: lock failed",
			err:  lockErr,
		},
		{
			name: "create schema migrations table",
			db:   &dbFake{createTableErr: createErr}, //nolint:exhaustruct
			want: "create schema migrations table: create failed",
			err:  createErr,
		},
		{
			name: "last version",
			db:   &dbFake{lastVersionErr: lastVersionErr}, //nolint:exhaustruct
			want: "last version: last version failed",
			err:  lastVersionErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := mig.New(mig.Migrations{}, tt.db)

			err := m.Migrate(context.Background())
			if err == nil {
				t.Fatal("Migrate() error=<nil>; want error")
			}

			if !errors.Is(err, tt.err) {
				t.Fatalf("Migrate() error=%v; want wrapped error", err)
			}

			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Migrate() error=%q; want %q", err, tt.want)
			}
		})
	}
}

func TestMigrateWrapsSetLastVersionError(t *testing.T) {
	t.Parallel()

	setErr := errors.New("set failed")
	db := &dbFake{setVersionErr: setErr} //nolint:exhaustruct
	m := mig.New(mig.Migrations{{
		Version: 3,
		Path:    "003-ok.sql",
		SQL:     "SELECT 1",
	}}, db)

	err := m.Migrate(context.Background())
	if !errors.Is(err, setErr) {
		t.Fatalf("Migrate() error=%v; want set error", err)
	}

	if !strings.Contains(err.Error(), "set last version 3: set failed") {
		t.Fatalf("Migrate() error=%q; want set last version context", err)
	}
}

func ExampleFromPgxPool() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, testDSN())
	if err != nil {
		panic(err)
	}

	defer pool.Close()

	migrator, cleanup, err := mig.FromPgxPool(migrations, pool, mig.WithCustomTable("eve"),
		mig.WithAcquireConnectionTimeout(time.Second))
	if err != nil {
		panic(err)
	}

	defer cleanup()

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}

func ExampleFromPgx() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	migrations, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, testDSN())
	if err != nil {
		panic(err)
	}

	defer func() {
		if err := conn.Close(ctx); err != nil {
			panic(err)
		}
	}()

	migrator := mig.FromPgx(migrations, conn, mig.WithCustomTable("trudy"))

	if err := migrator.Migrate(ctx); err != nil {
		panic(err)
	}

	// Output:
}
