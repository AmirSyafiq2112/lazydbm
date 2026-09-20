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
	SourceSaved           = "saved"
)

type Connection struct {
	Name         string `yaml:"name"`
	Engine       Engine `yaml:"engine"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	User         string `yaml:"user"`
	Database     string `yaml:"database,omitempty"`
	LastDatabase string `yaml:"last_used_db,omitempty"`
	Source       string `yaml:"source,omitempty"`
	NoPassword   bool   `yaml:"no_password,omitempty"`
}

func (c Connection) ID() string {
	return fmt.Sprintf("%s|%s|%d|%s", c.Engine, c.Host, c.Port, c.User)
}

func (c Connection) Short() string {
	eng := "pg"
	if c.Engine == EngineMySQL {
		eng = "my"
	}
	host := strings.TrimSpace(c.Host)
	if host == "" || host == "127.0.0.1" || host == "localhost" {
		return fmt.Sprintf("%s  %s@%d", eng, c.User, c.Port)
	}
	return fmt.Sprintf("%s  %s@%s:%d", eng, c.User, host, c.Port)
}

func (c Connection) ValidServer() bool {
	return c.Engine != "" && c.Host != "" && c.Port > 0 && c.User != ""
}

func (c Connection) Valid() bool {
	return c.ValidServer() && c.Database != ""
}

func (c Connection) WithDatabase(name string) Connection {
	c.Database = name
	return c
}

func (c Connection) ValidateFields() error {
	if !c.ValidServer() {
		return fmt.Errorf("incomplete connection")
	}
	for _, s := range []string{c.Host, c.User} {
		if err := rejectCLIUnsafe(s); err != nil {
			return err
		}
	}
	if c.Database != "" {
		if err := ValidateDatabase(c.Database); err != nil {
			return err
		}
	}
	return nil
}

func ValidateDatabase(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("invalid database name")
	}
	if err := rejectCLIUnsafe(name); err != nil {
		return err
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid database name")
	}
	return nil
}

func rejectCLIUnsafe(s string) error {
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("value must not start with -")
	}
	if strings.ContainsRune(s, 0) || strings.ContainsAny(s, "\r\n") {
		return fmt.Errorf("value contains invalid characters")
	}
	return nil
}

func (c Connection) Saved() bool {
	return c.Source == SourceSaved
}

func DefaultPort(e Engine) int {
	if e == EngineMySQL {
		return 3306
	}
	return 5432
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
	return normalizeFile(f), nil
}

func normalizeFile(f File) File {
	lastID, lastDB := splitLastUsed(f.LastUsed)
	if lastID != f.LastUsed {
		f.LastUsed = lastID
	}

	seen := map[string]int{}
	keep := make([]Connection, 0, len(f.Connections))
	for _, c := range f.Connections {
		if c.Database != "" && c.LastDatabase == "" {
			c.LastDatabase = c.Database
		}
		c.Database = ""
		if c.LastDatabase != "" && ValidateDatabase(c.LastDatabase) != nil {
			c.LastDatabase = ""
		}
		if err := c.ValidateFields(); err != nil {
			continue
		}
		id := c.ID()
		if idx, ok := seen[id]; ok {
			if c.Name != "" {
				keep[idx].Name = c.Name
			}
			if c.LastDatabase != "" {
				keep[idx].LastDatabase = c.LastDatabase
			}
			if c.Source != "" {
				keep[idx].Source = c.Source
			}
			continue
		}
		seen[id] = len(keep)
		keep = append(keep, c)
	}
	f.Connections = keep
	if lastDB != "" {
		if idx, ok := seen[lastID]; ok && ValidateDatabase(lastDB) == nil {
			keep[idx].LastDatabase = lastDB
			f.Connections = keep
		}
	}
	return f
}

func splitLastUsed(id string) (serverID, db string) {
	parts := strings.Split(id, "|")
	if len(parts) == 5 {
		return strings.Join(parts[:4], "|"), parts[4]
	}
	return id, ""
}

func Save(f File) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	for i := range f.Connections {
		f.Connections[i].Database = ""
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func (f *File) Upsert(c Connection) {
	if c.Database != "" && c.LastDatabase == "" {
		c.LastDatabase = c.Database
	}
	c.Database = ""
	id := c.ID()
	for i, existing := range f.Connections {
		if existing.ID() == id {
			if c.Name != "" {
				existing.Name = c.Name
			}
			if c.Source != "" {
				existing.Source = c.Source
			}
			if c.LastDatabase != "" {
				existing.LastDatabase = c.LastDatabase
			}
			existing.NoPassword = c.NoPassword
			existing.Database = ""
			f.Connections[i] = existing
			return
		}
	}
	f.Connections = append(f.Connections, c)
}

func (f *File) Remove(id string) bool {
	id, _ = splitLastUsed(id)
	for i, c := range f.Connections {
		if c.ID() != id {
			continue
		}
		f.Connections = append(f.Connections[:i], f.Connections[i+1:]...)
		if f.LastUsed == id {
			f.LastUsed = ""
		}
		return true
	}
	return false
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
