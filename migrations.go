package mig

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrInvalidVersion    = errors.New("invalid migration version")
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

func migrations(fS fs.FS, files []fs.DirEntry, directory string) (Migrations, error) {
	seen := make(map[uint64]string, len(files))
	ms := make(Migrations, 0, len(files))

	for _, file := range files {
		fileName := file.Name()
		ext := filepath.Ext(fileName)

		if file.IsDir() || ext != ".sql" {
			continue
		}

		id := numberPrefix(filepath.Base(fileName))

		if len(id) == 0 {
			return nil, fmt.Errorf(
				"%w: missing numeric prefix in %s",
				ErrInvalidVersion,
				filepath.Base(fileName),
			)
		}

		version, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: unparseable version in %s", ErrInvalidVersion, fileName)
		}
		if version == 0 || version > maxPostgresBigintVersion {
			return nil, fmt.Errorf(
				"%w: version must be between 1 and %d in %s",
				ErrInvalidVersion,
				maxPostgresBigintVersion,
				fileName,
			)
		}

		if first, ok := seen[version]; ok {
			return nil, fmt.Errorf(
				"%w: %s duplicates %s",
				ErrDuplicateVersion,
				fileName,
				first,
			)
		}

		name := strings.TrimPrefix(fileName, id)
		name = strings.TrimPrefix(name, "-")
		name = strings.TrimPrefix(name, "_")
		name = strings.TrimSuffix(name, ext)

		sql, err := fs.ReadFile(fS, path.Join(directory, fileName))
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}

		ms = append(ms, Migration{
			Version: version,
			Name:    name,
			Path:    fileName,
			SQL:     string(sql),
		})

		seen[version] = fileName
	}

	sort.Sort(ms)

	return ms, nil
}

func (ms Migrations) Len() int {
	return len(ms)
}

func (ms Migrations) Less(i, j int) bool {
	return ms[i].Version < ms[j].Version
}

func (ms Migrations) Swap(i, j int) {
	ms[i], ms[j] = ms[j], ms[i]
}

type Migration struct {
	Version uint64
	Name    string
	Path    string
	SQL     string
}

// Validate checks that migration versions are valid, unique, and strictly
// increasing. An empty migration set is valid because migrating it is a no-op.
// It returns ErrInvalidVersion, ErrDuplicateVersion, or ErrOutOfOrderVersion
// when the corresponding invariant is violated.
func (ms Migrations) Validate() error {
	seen := make(map[uint64]int, len(ms))
	for i, m := range ms {
		if m.Version == 0 || m.Version > maxPostgresBigintVersion {
			return fmt.Errorf("%w: %s", ErrInvalidVersion, describeMigration(i, m))
		}
		if first, ok := seen[m.Version]; ok {
			return fmt.Errorf(
				"%w: %s duplicates %s",
				ErrDuplicateVersion,
				describeMigration(i, m),
				describeMigration(first, ms[first]),
			)
		}
		seen[m.Version] = i
		if i == 0 {
			continue
		}

		previous := ms[i-1]
		if m.Version < previous.Version {
			return fmt.Errorf(
				"%w: %s follows %s",
				ErrOutOfOrderVersion,
				describeMigration(i, m),
				describeMigration(i-1, previous),
			)
		}
	}

	return nil
}

func describeMigration(index int, migration Migration) string {
	description := fmt.Sprintf("migration at index %d", index)
	if migration.Path != "" {
		description += " from " + migration.Path
	}

	return fmt.Sprintf("%s with version %d", description, migration.Version)
}

// TargetVersion validates the migration set and returns its newest version. It
// returns ErrNoMigrations if the set is empty, or an error from Validate if the
// set is invalid.
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
