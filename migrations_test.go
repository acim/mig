package mig_test

import (
	"embed"
	"sort"
	"testing"

	"go.acim.net/mig"
)

//go:embed migrations
var ms embed.FS

var _ sort.Interface = (*mig.Migrations)(nil)

func TestFromFiles(t *testing.T) {
	want := mig.Migrations{
		{
			Version: 1,
			Name:    "",
			Path:    "1.sql",
		},
		{
			Version: 2,
			Name:    "",
			Path:    "02.sql",
		},
	}

	got, err := mig.FromEmbedFS(ms, "valid")
	if err != nil {
		t.Fatal(err)
	}

	for i := range got {
		if got[i].Version != want[i].Version ||
			got[i].Name != want[i].Name ||
			got[i].Path != want[i].Path {
			t.Errorf("FromFiles() = %v; want %v", got, want)
		}
	}
}
