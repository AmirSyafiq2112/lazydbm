package discover

import (
	"path/filepath"
	"testing"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

func TestScanLaravelComposeRewritesServiceHost(t *testing.T) {
	dir := filepath.Join("testdata", "laravel")
	res, err := Scan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0] != filepath.Join("database", "dump.sql") {
		t.Fatalf("files = %#v", res.Files)
	}

	var pg *config.Connection
	for i := range res.Connections {
		c := res.Connections[i]
		if c.Database == "epbt" && c.Engine == config.EnginePostgres {
			pg = &res.Connections[i]
			break
		}
	}
	if pg == nil {
		t.Fatalf("postgres connection not found: %#v", res.Connections)
	}
	if pg.Host != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1 (compose service rewritten)", pg.Host)
	}
	if pg.Port != 5433 {
		t.Fatalf("port = %d, want 5433 (published host port)", pg.Port)
	}
	if res.EnvPasswords[pg.ID()] != "secret" {
		t.Fatalf("password missing for %s: %#v", pg.ID(), res.EnvPasswords)
	}
}

func TestScanMySQLEnv(t *testing.T) {
	dir := filepath.Join("testdata", "mysql")
	res, err := Scan(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Connections) != 1 {
		t.Fatalf("connections = %#v", res.Connections)
	}
	c := res.Connections[0]
	if c.Engine != config.EngineMySQL || c.Database != "shop" || c.User != "root" {
		t.Fatalf("conn = %#v", c)
	}
	if res.EnvPasswords[c.ID()] != "p@ss word" {
		t.Fatalf("password = %q", res.EnvPasswords[c.ID()])
	}
}

func TestScanMergesSaved(t *testing.T) {
	dir := t.TempDir()
	saved := []config.Connection{{
		Name:     "saved",
		Engine:   config.EnginePostgres,
		Host:     "db.example",
		Port:     5432,
		User:     "u",
		Database: "d",
		Source:   "config",
	}}
	res, err := Scan(dir, saved)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Connections) != 1 || res.Connections[0].Host != "db.example" {
		t.Fatalf("connections = %#v", res.Connections)
	}
}

func TestParseDatabaseURL(t *testing.T) {
	c, pw, ok := parseDatabaseURL("postgres://app:s3cret@localhost:5434/epbt", ".env")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if c.Engine != config.EnginePostgres || c.Host != "localhost" || c.Port != 5434 || c.User != "app" || c.Database != "epbt" {
		t.Fatalf("conn = %#v", c)
	}
	if pw != "s3cret" {
		t.Fatalf("pw = %q", pw)
	}
}

func TestIsEnvFile(t *testing.T) {
	if !isEnvFile(".env") || !isEnvFile(".env.local") {
		t.Fatal("expected .env files accepted")
	}
	if isEnvFile(".env.example") || isEnvFile("docker-compose.yml") {
		t.Fatal("unexpected env file match")
	}
}
