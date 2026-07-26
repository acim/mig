package mig_test

import (
	"embed"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	if want := "missing numeric prefix in broken.sql"; !strings.Contains(err.Error(), want) {
		t.Fatalf("FromDir() error=%q; want it to contain %q", err, want)
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
			want := mig.ErrInvalidVersion.Error() +
				": version must be between 1 and 9223372036854775807 in " + name
			if err.Error() != want {
				t.Fatalf("FromDir() error=%q; want %q", err, want)
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
	want := mig.ErrInvalidVersion.Error() + ": unparseable version in " + name
	if err.Error() != want {
		t.Fatalf("FromDir() error=%q; want %q", err, want)
	}
}

func TestFromDirReturnsInvalidVersionErrorForPostgresBigintOverflow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	name := "9223372036854775808-too-large.sql"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	_, err := mig.FromDir(dir)
	if !errors.Is(err, mig.ErrInvalidVersion) {
		t.Fatalf("FromDir() error=%v; want invalid version error", err)
	}
	want := mig.ErrInvalidVersion.Error() +
		": version must be between 1 and 9223372036854775807 in " + name
	if err.Error() != want {
		t.Fatalf("FromDir() error=%q; want %q", err, want)
	}
}

func TestFromDirIgnoresNonMigrationEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "001-dir.sql"), 0o700); err != nil {
		t.Fatalf("make migration-like directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "001-real.sql"), []byte("SELECT 1"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	got, err := mig.FromDir(dir)
	if err != nil {
		t.Fatalf("FromDir(): %v", err)
	}

	if len(got) != 1 || got[0].Path != "001-real.sql" {
		t.Fatalf("FromDir() migrations=%#v; want only 001-real.sql", got)
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
	want := mig.ErrDuplicateVersion.Error() + ": 1-two.sql duplicates 001-one.sql"
	if err.Error() != want {
		t.Fatalf("FromDir() error=%q; want %q", err, want)
	}
}

func TestMigrationsValidateRejectsDuplicateVersion(t *testing.T) {
	t.Parallel()

	migrations := mig.Migrations{
		{Version: 1, Path: "001-one.sql"},
		{Version: 1, Path: "001-duplicate.sql"},
	}

	err := migrations.Validate()
	if !errors.Is(err, mig.ErrDuplicateVersion) {
		t.Fatalf("Validate() error=%v; want duplicate version error", err)
	}
}

func TestMigrationsValidateRejectsNonAdjacentDuplicateVersion(t *testing.T) {
	t.Parallel()

	migrations := mig.Migrations{
		{Version: 1, Path: "001-first.sql"},
		{Version: 2, Path: "002-second.sql"},
		{Version: 1, Path: "001-duplicate.sql"},
	}

	err := migrations.Validate()
	if !errors.Is(err, mig.ErrDuplicateVersion) {
		t.Fatalf("Validate() error=%v; want duplicate version error", err)
	}
	want := mig.ErrDuplicateVersion.Error() +
		": migration at index 2 from 001-duplicate.sql with version 1 duplicates " +
		"migration at index 0 from 001-first.sql with version 1"
	if err.Error() != want {
		t.Fatalf("Validate() error=%q; want %q", err, want)
	}
}

func TestMigrationsValidateRejectsOutOfOrderVersion(t *testing.T) {
	t.Parallel()

	migrations := mig.Migrations{
		{Version: 2, Path: "002-second.sql"},
		{Version: 1, Path: "001-first.sql"},
	}

	err := migrations.Validate()
	if !errors.Is(err, mig.ErrOutOfOrderVersion) {
		t.Fatalf("Validate() error=%v; want out-of-order version error", err)
	}
}

func TestMigrationsValidateAcceptsEmptySet(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		migrations mig.Migrations
	}{
		{name: "nil", migrations: nil},
		{name: "empty", migrations: mig.Migrations{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.migrations.Validate(); err != nil {
				t.Fatalf("Validate() error=%v; want nil", err)
			}
		})
	}
}

func TestMigrationsValidateDescribesPathlessInvalidVersion(t *testing.T) {
	t.Parallel()

	err := (mig.Migrations{{Version: 0}}).Validate()
	if !errors.Is(err, mig.ErrInvalidVersion) {
		t.Fatalf("Validate() error=%v; want invalid version error", err)
	}
	for _, want := range []string{"migration at index 0", "version 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error=%q; want it to contain %q", err, want)
		}
	}
}

func TestMigrationsValidateDescribesPathlessOutOfOrderVersions(t *testing.T) {
	t.Parallel()

	err := (mig.Migrations{{Version: 2}, {Version: 1}}).Validate()
	if !errors.Is(err, mig.ErrOutOfOrderVersion) {
		t.Fatalf("Validate() error=%v; want out-of-order version error", err)
	}
	want := mig.ErrOutOfOrderVersion.Error() +
		": migration at index 1 with version 1 follows " +
		"migration at index 0 with version 2"
	if err.Error() != want {
		t.Fatalf("Validate() error=%q; want %q", err, want)
	}
}

func TestMigrationsTargetVersion(t *testing.T) {
	t.Parallel()

	migrations := mig.Migrations{
		{Version: 2, Path: "002-second.sql"},
		{Version: 7, Path: "007-seventh.sql"},
	}

	version, err := migrations.TargetVersion()
	if err != nil {
		t.Fatalf("TargetVersion() error=%v", err)
	}
	if version != 7 {
		t.Fatalf("TargetVersion()=%d; want 7", version)
	}
}

func TestMigrationsTargetVersionAcceptsSingleMigration(t *testing.T) {
	t.Parallel()

	version, err := (mig.Migrations{{Version: 7, Path: "007-only.sql"}}).TargetVersion()
	if err != nil {
		t.Fatalf("TargetVersion() error=%v", err)
	}
	if version != 7 {
		t.Fatalf("TargetVersion()=%d; want 7", version)
	}
}

func TestMigrationsTargetVersionRejectsEmptySet(t *testing.T) {
	t.Parallel()

	version, err := (mig.Migrations{}).TargetVersion()
	if !errors.Is(err, mig.ErrNoMigrations) {
		t.Fatalf("TargetVersion() error=%v; want no migrations error", err)
	}
	if version != 0 {
		t.Fatalf("TargetVersion()=%d; want 0", version)
	}
}

func TestMigrationsTargetVersionRejectsInvalidSet(t *testing.T) {
	t.Parallel()

	migrations := mig.Migrations{
		{Version: 2, Path: "002-second.sql"},
		{Version: 1, Path: "001-first.sql"},
	}

	version, err := migrations.TargetVersion()
	if !errors.Is(err, mig.ErrOutOfOrderVersion) {
		t.Fatalf("TargetVersion() error=%v; want out-of-order version error", err)
	}
	if version != 0 {
		t.Fatalf("TargetVersion()=%d; want 0", version)
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
