package mig_test

import (
	"embed"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"go.acim.net/mig"
)

//go:embed migrations
var ms embed.FS

var _ sort.Interface = (*mig.Migrations)(nil)

func TestFromDir(t *testing.T) {
	t.Parallel()

	want := want()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	got, err := mig.FromDir(filepath.Join(wd, "migrations"))
	if err != nil {
		t.Fatalf("from directory: %v", err)
	}

	assertMigrations(t, got, want)
}

func TestFromEmbedFS(t *testing.T) {
	t.Parallel()

	want := want()

	got, err := mig.FromEmbedFS(ms, "migrations")
	if err != nil {
		t.Fatalf("from embed fs: %v", err)
	}

	assertMigrations(t, got, want)
}

func TestFromDirReturnsReadDirError(t *testing.T) {
	t.Parallel()

	_, err := mig.FromDir(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("FromDir() error=<nil>; want error")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FromDir() error=%v; want not exist error", err)
	}
}

func TestFromEmbedFSReturnsReadDirError(t *testing.T) {
	t.Parallel()

	_, err := mig.FromEmbedFS(ms, "missing")
	if err == nil {
		t.Fatal("FromEmbedFS() error=<nil>; want error")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FromEmbedFS() error=%v; want not exist error", err)
	}
}

func TestFromDirReturnsInvalidVersionError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.sql"), []byte("SELECT 1"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	_, err := mig.FromDir(dir)
	if !errors.Is(err, mig.ErrInvalidVersion) {
		t.Fatalf("FromDir() error=%v; want invalid version error", err)
	}
}

func TestFromDirReturnsInvalidVersionErrorForZeroVersion(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"0.sql", "000-initial.sql"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1"), 0o600); err != nil {
				t.Fatalf("write migration: %v", err)
			}

			_, err := mig.FromDir(dir)
			if !errors.Is(err, mig.ErrInvalidVersion) {
				t.Fatalf("FromDir() error=%v; want invalid version error", err)
			}
		})
	}
}

func TestFromDirReturnsInvalidVersionErrorForOverflowingVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	name := "18446744073709551616-too-large.sql"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	_, err := mig.FromDir(dir)
	if !errors.Is(err, mig.ErrInvalidVersion) {
		t.Fatalf("FromDir() error=%v; want invalid version error", err)
	}
}

func TestFromDirReturnsDuplicateVersionError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"001-one.sql", "1-two.sql"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1"), 0o600); err != nil {
			t.Fatalf("write migration %s: %v", name, err)
		}
	}

	_, err := mig.FromDir(dir)
	if !errors.Is(err, mig.ErrDuplicateVersion) {
		t.Fatalf("FromDir() error=%v; want duplicate version error", err)
	}
}

func want() mig.Migrations {
	return mig.Migrations{
		{
			Version: 1,
			Name:    "",
			Path:    "1.sql",
			SQL: `CREATE TABLE IF NOT EXISTS users (
	user_id serial PRIMARY KEY,
	username VARCHAR (50) UNIQUE NOT NULL
);

INSERT INTO users (username) VALUES ('zika');

DROP TABLE users;
`,
		},
		{
			Version: 2,
			Name:    "",
			Path:    "02.sql",
			SQL: `CREATE TABLE accounts (
	user_id serial PRIMARY KEY,
	username VARCHAR ( 50 ) UNIQUE NOT NULL,
	password VARCHAR ( 50 ) NOT NULL,
	email VARCHAR ( 255 ) UNIQUE NOT NULL,
	created_on TIMESTAMP NOT NULL,
    last_login TIMESTAMP
);

DROP TABLE accounts;
`,
		},
	}
}

func assertMigrations(t *testing.T, got, want mig.Migrations) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len(migrations)=%d; want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("migration[%d]=%#v; want %#v", i, got[i], want[i])
		}
	}
}
