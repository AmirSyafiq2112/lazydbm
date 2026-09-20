package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
)

type Connection struct {
	Name     string `yaml:"name"`
	Engine   Engine `yaml:"engine"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Database string `yaml:"database"`
	Source   string `yaml:"source,omitempty"`
}

func (c Connection) ID() string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", c.Engine, c.Host, c.Port, c.User, c.Database)
}

func (c Connection) Short() string {
	eng := "pg"
	if c.Engine == EngineMySQL {
		eng = "my"
	}
	return fmt.Sprintf("%s  %s@%s:%d/%s", eng, c.User, c.Host, c.Port, c.Database)
}

func (c Connection) Valid() bool {
	return c.Engine != "" && c.Host != "" && c.Port > 0 && c.User != "" && c.Database != ""
}

type File struct {
	LastUsed    string       `yaml:"last_used,omitempty"`
	Connections []Connection `yaml:"connections,omitempty"`
}

func Path() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "lazydbm", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "lazydbm", "config.yaml"), nil
}

func Load() (File, error) {
	path, err := Path()
	if err != nil {
		return File{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, err
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	return f, nil
}

func Save(f File) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func (f *File) Upsert(c Connection) {
	id := c.ID()
	for i, existing := range f.Connections {
		if existing.ID() == id {
			if c.Name != "" {
				existing.Name = c.Name
			}
			if c.Source != "" {
				existing.Source = c.Source
			}
			f.Connections[i] = existing
			return
		}
	}
	f.Connections = append(f.Connections, c)
}

func NormalizeEngine(s string) (Engine, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "postgres", "postgresql", "pgsql", "pg":
		return EnginePostgres, true
	case "mysql", "mariadb", "percona":
		return EngineMySQL, true
	default:
		return "", false
	}
}
