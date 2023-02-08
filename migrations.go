package mig

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrInvalidVersion   = errors.New("invalid migration version prefix")
	ErrDuplicateVersion = errors.New("duplicate version")
)

type Migrations []Migration

func FromEmbedFS(fs embed.FS, path string) (Migrations, error) {
	files, err := fs.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

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

		version, _ := strconv.ParseUint(id, 10, 64)

		if seen[version] {
			return nil, fmt.Errorf("%w: %d", ErrDuplicateVersion, version)
		}

		name := strings.TrimPrefix(fileName, id)
		name = strings.TrimPrefix(name, "-")
		name = strings.TrimPrefix(name, "_")
		name = strings.TrimSuffix(name, ext)

		sql, err := fs.ReadFile(filepath.Join(path, fileName))
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
