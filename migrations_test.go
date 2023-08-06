package mig_test

import (
	"embed"
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
