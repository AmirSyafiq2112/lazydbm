package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var skipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	".idea":        {},
	".vscode":      {},
}

func dumpFiles(cwd string) ([]string, error) {
	var files []string
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
				if s.IsDir() {
					continue
				}
				if isDump(s.Name()) {
					files = append(files, filepath.Join(name, s.Name()))
				}
			}
			continue
		}
		if isDump(name) {
			files = append(files, name)
		}
	}
	sort.Strings(files)
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
