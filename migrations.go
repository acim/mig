package mig

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrInvalidVersion    = errors.New("invalid migration version prefix")
	ErrDuplicateVersion  = errors.New("duplicate version")
	ErrOutOfOrderVersion = errors.New("migration version out of order")
	ErrNoMigrations      = errors.New("no migrations")
)

const maxPostgresBigintVersion = uint64(1<<63 - 1)

type Migrations []Migration

func FromDir(path string) (Migrations, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	fs := os.DirFS(path)

	return migrations(fs, files, "")
}

func FromEmbedFS(fs embed.FS, path string) (Migrations, error) {
	files, err := fs.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	return migrations(fs, files, path)
}

func migrations(fS fs.FS, files []fs.DirEntry, path string) (Migrations, error) {
	seen := make(map[uint64]bool, len(files))
	ms := make(Migrations, 0, len(files))

	for _, file := range files {
		fileName := file.Name()
		ext := filepath.Ext(fileName)

		if file.IsDir() || ext != ".sql" {
			continue
		}

		id := numberPrefix(filepath.Base(fileName))

		if len(id) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidVersion, filepath.Base(fileName))
		}

		version, err := strconv.ParseUint(id, 10, 64)
		if err != nil || version == 0 || version > maxPostgresBigintVersion {
			return nil, fmt.Errorf("%w: %s", ErrInvalidVersion, filepath.Base(fileName))
		}

		if seen[version] {
			return nil, fmt.Errorf("%w: %d", ErrDuplicateVersion, version)
		}

		name := strings.TrimPrefix(fileName, id)
		name = strings.TrimPrefix(name, "-")
		name = strings.TrimPrefix(name, "_")
		name = strings.TrimSuffix(name, ext)

		sql, err := fs.ReadFile(fS, filepath.Join(path, fileName))
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}

		ms = append(ms, Migration{
			Version: version,
			Name:    name,
			Path:    fileName,
			SQL:     string(sql),
		})

		seen[version] = true
	}

	sort.Sort(&ms)

	return ms, nil
}

func (ms *Migrations) Len() int {
	return len(*ms)
}

func (ms *Migrations) Less(i, j int) bool {
	return (*ms)[i].Version < (*ms)[j].Version
}

func (ms *Migrations) Swap(i, j int) {
	(*ms)[i], (*ms)[j] = (*ms)[j], (*ms)[i]
}

type Migration struct {
	Version uint64
	Name    string
	Path    string
	SQL     string
}

// Validate checks that migration versions are valid and strictly increasing.
func (ms Migrations) Validate() error {
	for i, m := range ms {
		if m.Version == 0 || m.Version > maxPostgresBigintVersion {
			return fmt.Errorf("%w: %s", ErrInvalidVersion, m.Path)
		}
		if i == 0 {
			continue
		}

		previous := ms[i-1]
		if m.Version == previous.Version {
			return fmt.Errorf("%w: %d", ErrDuplicateVersion, m.Version)
		}
		if m.Version < previous.Version {
			return fmt.Errorf(
				"%w: migration %d from %s follows migration %d from %s",
				ErrOutOfOrderVersion,
				m.Version,
				m.Path,
				previous.Version,
				previous.Path,
			)
		}
	}

	return nil
}

// TargetVersion returns the newest version in a valid, non-empty migration set.
func (ms Migrations) TargetVersion() (uint64, error) {
	if len(ms) == 0 {
		return 0, ErrNoMigrations
	}
	if err := ms.Validate(); err != nil {
		return 0, err
	}

	return ms[len(ms)-1].Version, nil
}

func numberPrefix(s string) string {
	var r bytes.Buffer

	for i := range s {
		if s[i] < 47 || s[i] > 57 {
			break
		}

		r.WriteByte(s[i])
	}

	return r.String()
}
