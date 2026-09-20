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

func TestNormalizeEngine(t *testing.T) {
	cases := map[string]Engine{
		"pgsql":      EnginePostgres,
		"PostgreSQL": EnginePostgres,
		"mariadb":    EngineMySQL,
		"mysql":      EngineMySQL,
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
