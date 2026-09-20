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
	want := "postgres|127.0.0.1|5432|app"
	if c.ID() != want {
		t.Fatalf("ID = %q, want %q", c.ID(), want)
	}
	other := c
	other.Database = "other"
	if other.ID() != c.ID() {
		t.Fatal("server ID must ignore database")
	}
}

func TestShortAndValid(t *testing.T) {
	pg := Connection{Engine: EnginePostgres, Host: "h", Port: 1, User: "u", Database: "d"}
	if pg.Short() != "pg  u@h:1" {
		t.Fatalf("short pg = %q", pg.Short())
	}
	local := Connection{Engine: EnginePostgres, Host: "127.0.0.1", Port: 5432, User: "postgres"}
	if local.Short() != "pg  postgres@5432" {
		t.Fatalf("short local = %q", local.Short())
	}
	my := Connection{Engine: EngineMySQL, Host: "h", Port: 3306, User: "root", Database: "shop"}
	if my.Short() != "my  root@h:3306" {
		t.Fatalf("short my = %q", my.Short())
	}
	if !pg.Valid() {
		t.Fatal("expected valid")
	}
	if !local.ValidServer() || local.Valid() {
		t.Fatal("server without database is ValidServer only")
	}
	if (Connection{Engine: EnginePostgres, Host: "h", Port: 0, User: "u", Database: "d"}).Valid() {
		t.Fatal("port 0 is invalid")
	}
	if (Connection{}).Valid() || (Connection{}).ValidServer() {
		t.Fatal("empty is invalid")
	}
}

func TestWithDatabase(t *testing.T) {
	c := Connection{Engine: EnginePostgres, Host: "h", Port: 1, User: "u"}
	got := c.WithDatabase("epbt")
	if got.Database != "epbt" || c.Database != "" {
		t.Fatalf("with = %#v orig = %#v", got, c)
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

func TestSavedAndDefaultPort(t *testing.T) {
	if !(Connection{Source: SourceSaved}).Saved() {
		t.Fatal("saved")
	}
	if (Connection{Source: ".env"}).Saved() {
		t.Fatal(".env is not saved")
	}
	if DefaultPort(EngineMySQL) != 3306 || DefaultPort(EnginePostgres) != 5432 {
		t.Fatal("default ports")
	}
}

func TestRemove(t *testing.T) {
	a := Connection{Engine: EnginePostgres, Host: "h", Port: 1, User: "u"}
	b := Connection{Engine: EngineMySQL, Host: "h", Port: 1, User: "u"}
	f := File{LastUsed: a.ID(), Connections: []Connection{a, b}}
	if !f.Remove(a.ID()) {
		t.Fatal("expected remove")
	}
	if f.LastUsed != "" || len(f.Connections) != 1 || f.Connections[0].ID() != b.ID() {
		t.Fatalf("%#v", f)
	}
	if f.Remove("missing") {
		t.Fatal("missing")
	}
}

func TestUpsert(t *testing.T) {
	f := File{}
	c := Connection{Engine: EnginePostgres, Host: "h", Port: 1, User: "u", Name: "one", Source: "a"}
	f.Upsert(c)
	c.Name = "two"
	c.Source = "b"
	c.LastDatabase = "epbt"
	f.Upsert(c)
	if len(f.Connections) != 1 {
		t.Fatalf("len = %d", len(f.Connections))
	}
	if f.Connections[0].Name != "two" || f.Connections[0].Source != "b" || f.Connections[0].LastDatabase != "epbt" {
		t.Fatalf("upsert = %#v", f.Connections[0])
	}
	if f.Connections[0].Database != "" {
		t.Fatal("saved server must omit database identity")
	}
	c.NoPassword = true
	f.Upsert(c)
	if !f.Connections[0].NoPassword {
		t.Fatal("upsert must copy no_password")
	}
	c.NoPassword = false
	f.Upsert(c)
	if f.Connections[0].NoPassword {
		t.Fatal("upsert must clear no_password")
	}
	f.Upsert(Connection{Engine: EngineMySQL, Host: "h", Port: 1, User: "u", Name: "x"})
	if len(f.Connections) != 2 {
		t.Fatalf("len after insert = %d", len(f.Connections))
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	f := File{
		LastUsed: "postgres|127.0.0.1|5432|app",
		Connections: []Connection{{
			Name:         "epbt",
			Engine:       EnginePostgres,
			Host:         "127.0.0.1",
			Port:         5432,
			User:         "app",
			LastDatabase: "epbt",
			Source:       SourceSaved,
			NoPassword:   true,
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
	if got.Connections[0].LastDatabase != "epbt" || got.Connections[0].Database != "" {
		t.Fatalf("last db = %#v", got.Connections[0])
	}
	if !got.Connections[0].NoPassword {
		t.Fatal("no_password should round-trip")
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

func TestLoadMigratesLegacyDatabaseIdentity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("last_used: postgres|127.0.0.1|5432|app|epbt\nconnections:\n" +
		"  - name: one\n    engine: postgres\n    host: 127.0.0.1\n    port: 5432\n    user: app\n    database: epbt\n    source: saved\n" +
		"  - name: two\n    engine: postgres\n    host: 127.0.0.1\n    port: 5432\n    user: app\n    database: other\n    source: saved\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.LastUsed != "postgres|127.0.0.1|5432|app" {
		t.Fatalf("LastUsed = %q", got.LastUsed)
	}
	if len(got.Connections) != 1 {
		t.Fatalf("same server must merge: %#v", got.Connections)
	}
	if got.Connections[0].Database != "" || got.Connections[0].LastDatabase != "epbt" {
		t.Fatalf("migrated = %#v", got.Connections[0])
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

func TestValidateFields(t *testing.T) {
	ok := Connection{Engine: EnginePostgres, Host: "h", Port: 1, User: "u"}
	if err := ok.ValidateFields(); err != nil {
		t.Fatal(err)
	}
	ok.Database = "d"
	if err := ok.ValidateFields(); err != nil {
		t.Fatal(err)
	}
	evil := ok
	evil.User = "--evil"
	if err := evil.ValidateFields(); err == nil {
		t.Fatal("user --evil must fail")
	}
	evil = ok
	evil.Host = "-h"
	if err := evil.ValidateFields(); err == nil {
		t.Fatal("host starting with - must fail")
	}
	evil = ok
	evil.Database = "db\nname"
	if err := evil.ValidateFields(); err == nil {
		t.Fatal("newline in database must fail")
	}
	evil = ok
	evil.Host = "h\x00st"
	if err := evil.ValidateFields(); err == nil {
		t.Fatal("NUL in host must fail")
	}
	evil = ok
	evil.User = "u\rser"
	if err := evil.ValidateFields(); err == nil {
		t.Fatal("CR in user must fail")
	}
	evil = ok
	evil.Database = "../etc"
	if err := evil.ValidateFields(); err == nil {
		t.Fatal(".. in database must fail")
	}
	evil = ok
	evil.Database = "a/b"
	if err := evil.ValidateFields(); err == nil {
		t.Fatal("/ in database must fail")
	}
	if err := ValidateDatabase("-bad"); err == nil {
		t.Fatal("leading - in database must fail")
	}
	if err := ValidateDatabase(`a\b`); err == nil {
		t.Fatal(`\ in database must fail`)
	}
}

func TestLoadSkipsInvalidConnections(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	good := Connection{Engine: EnginePostgres, Host: "127.0.0.1", Port: 5432, User: "app"}
	if err := Save(File{Connections: []Connection{
		good,
		{Engine: EnginePostgres, Host: "h", Port: 1, User: "--evil"},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Connections) != 1 || got.Connections[0].ID() != good.ID() {
		t.Fatalf("got %#v", got.Connections)
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
