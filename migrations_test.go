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

	for i := range got {
		if got[i].Version != want[i].Version ||
			got[i].Name != want[i].Name ||
			got[i].Path != want[i].Path {
			t.Errorf("FromFiles() = %v; want %v", got, want)
		}
	}
}

func TestFromEmbedFS(t *testing.T) {
	t.Parallel()

	want := want()

	got, err := mig.FromEmbedFS(ms, "migrations")
	if err != nil {
		t.Fatalf("from embed fs: %v", err)
	}

	for i := range got {
		if got[i].Version != want[i].Version ||
			got[i].Name != want[i].Name ||
			got[i].Path != want[i].Path {
			t.Errorf("FromFiles() = %v; want %v", got, want)
		}
	}
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
		{ //nolint:exhaustruct
			Version: 1,
			Name:    "",
			Path:    "1.sql",
		},
		{ //nolint:exhaustruct
			Version: 2,
			Name:    "",
			Path:    "02.sql",
		},
	}
}
