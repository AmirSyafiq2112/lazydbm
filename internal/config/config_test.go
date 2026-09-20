package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionID(t *testing.T) {
	c := Connection{
		Engine:   EnginePostgres,
		Host:     "127.0.0.1",
		Port:     5432,
		User:     "app",
		Database: "epbt",
	}
	want := "postgres|127.0.0.1|5432|app|epbt"
	if c.ID() != want {
		t.Fatalf("ID = %q, want %q", c.ID(), want)
	}
}

func TestShortAndValid(t *testing.T) {
	pg := Connection{Engine: EnginePostgres, Host: "h", Port: 1, User: "u", Database: "d"}
	if pg.Short() != "pg  u@h:1/d" {
		t.Fatalf("short pg = %q", pg.Short())
	}
	my := Connection{Engine: EngineMySQL, Host: "h", Port: 3306, User: "root", Database: "shop"}
	if my.Short() != "my  root@h:3306/shop" {
		t.Fatalf("short my = %q", my.Short())
	}
	if !pg.Valid() {
		t.Fatal("expected valid")
	}
	if (Connection{Engine: EnginePostgres, Host: "h", Port: 0, User: "u", Database: "d"}).Valid() {
		t.Fatal("port 0 is invalid")
	}
	if (Connection{}).Valid() {
		t.Fatal("empty is invalid")
	}
}

func TestNormalizeEngine(t *testing.T) {
	cases := map[string]Engine{
		"pgsql":      EnginePostgres,
		"PostgreSQL": EnginePostgres,
		"mariadb":    EngineMySQL,
		"mysql":      EngineMySQL,
		"percona":    EngineMySQL,
		"pg":         EnginePostgres,
	}
	for in, want := range cases {
		got, ok := NormalizeEngine(in)
		if !ok || got != want {
			t.Fatalf("NormalizeEngine(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	if _, ok := NormalizeEngine("sqlite"); ok {
		t.Fatal("sqlite should be unsupported")
	}
}

func TestUpsert(t *testing.T) {
	f := File{}
	c := Connection{Engine: EnginePostgres, Host: "h", Port: 1, User: "u", Database: "d", Name: "one", Source: "a"}
	f.Upsert(c)
	c.Name = "two"
	c.Source = "b"
	f.Upsert(c)
	if len(f.Connections) != 1 {
		t.Fatalf("len = %d", len(f.Connections))
	}
	if f.Connections[0].Name != "two" || f.Connections[0].Source != "b" {
		t.Fatalf("upsert = %#v", f.Connections[0])
	}
	f.Upsert(Connection{Engine: EngineMySQL, Host: "h", Port: 1, User: "u", Database: "other", Name: "x"})
	if len(f.Connections) != 2 {
		t.Fatalf("len after insert = %d", len(f.Connections))
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	f := File{
		LastUsed: "postgres|127.0.0.1|5432|app|epbt",
		Connections: []Connection{{
			Name:     "epbt",
			Engine:   EnginePostgres,
			Host:     "127.0.0.1",
			Port:     5432,
			User:     "app",
			Database: "epbt",
			Source:   ".env",
		}},
	}
	if err := Save(f); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.LastUsed != f.LastUsed {
		t.Fatalf("LastUsed = %q, want %q", got.LastUsed, f.LastUsed)
	}
	if len(got.Connections) != 1 || got.Connections[0].ID() != f.Connections[0].ID() {
		t.Fatalf("connections = %#v", got.Connections)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(path)) != dir {
		t.Fatalf("path %q not under %q", path, dir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if f.LastUsed != "" || len(f.Connections) != 0 {
		t.Fatalf("expected empty config, got %#v", f)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("last_used: [unterminated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected yaml error")
	}
}

func TestPathHomeFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != "config.yaml" {
		t.Fatalf("path = %q", path)
	}
}
