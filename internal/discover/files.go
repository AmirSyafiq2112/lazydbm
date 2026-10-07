package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var skipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	".idea":        {},
	".vscode":      {},
}

// Files returns dump files in cwd and one level of subdirectories.
func Files(cwd string) ([]string, error) {
	return dumpFiles(cwd)
}

func dumpFiles(cwd string) ([]string, error) {
	type item struct {
		path string
		mod  time.Time
	}
	var items []item
	add := func(rel string, info os.FileInfo) {
		mod := time.Time{}
		if info != nil {
			mod = info.ModTime()
		}
		items = append(items, item{path: rel, mod: mod})
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if _, skip := skipDirs[name]; skip || strings.HasPrefix(name, ".") {
				continue
			}
			sub, err := os.ReadDir(filepath.Join(cwd, name))
			if err != nil {
				continue
			}
			for _, s := range sub {
				if s.IsDir() || !isDump(s.Name()) {
					continue
				}
				info, _ := s.Info()
				add(filepath.Join(name, s.Name()), info)
			}
			continue
		}
		if isDump(name) {
			info, _ := e.Info()
			add(name, info)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].mod.Equal(items[j].mod) {
			return items[i].mod.After(items[j].mod)
		}
		return items[i].path < items[j].path
	})
	files := make([]string, len(items))
	for i, item := range items {
		files[i] = item.path
	}
	return files, nil
}

func isDump(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".sql", ".dump", ".backup", ".pgdump":
		return true
	default:
		return false
	}
}
