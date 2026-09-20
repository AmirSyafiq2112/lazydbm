package discover

import (
	"os"
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

func TestScanDatabaseURL(t *testing.T) {
	res, err := Scan(filepath.Join("testdata", "url"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Connections) != 1 {
		t.Fatalf("connections = %#v", res.Connections)
	}
	c := res.Connections[0]
	if c.Engine != config.EngineMySQL || c.Host != "db.local" || c.Port != 3307 || c.User != "shop" || c.Database != "catalog" {
		t.Fatalf("conn = %#v", c)
	}
	if res.EnvPasswords[c.ID()] != "s3cret" {
		t.Fatalf("pw = %q", res.EnvPasswords[c.ID()])
	}
}

func TestScanComposeMySQLListEnv(t *testing.T) {
	res, err := Scan(filepath.Join("testdata", "compose-mysql"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Connections) != 1 {
		t.Fatalf("connections = %#v", res.Connections)
	}
	c := res.Connections[0]
	if c.Engine != config.EngineMySQL || c.Database != "shop" || c.User != "app" || c.Port != 3307 || c.Host != "127.0.0.1" {
		t.Fatalf("conn = %#v", c)
	}
	if res.EnvPasswords[c.ID()] != "secret" {
		t.Fatalf("pw = %q", res.EnvPasswords[c.ID()])
	}
}

func TestScanQuotedEnvLocal(t *testing.T) {
	res, err := Scan(filepath.Join("testdata", "quoted"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Connections) != 1 {
		t.Fatalf("connections = %#v", res.Connections)
	}
	c := res.Connections[0]
	if c.Port != 5432 {
		t.Fatalf("invalid port should fall back, got %d", c.Port)
	}
	if res.EnvPasswords[c.ID()] != "plain" {
		t.Fatalf("pw = %q", res.EnvPasswords[c.ID()])
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

func TestScanMissingDir(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Fatal("expected error")
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
	if _, _, ok := parseDatabaseURL("://bad", ".env"); ok {
		t.Fatal("bad url")
	}
	if _, _, ok := parseDatabaseURL("sqlite://x", ".env"); ok {
		t.Fatal("sqlite")
	}
	if _, _, ok := parseDatabaseURL("postgres://localhost/", ".env"); ok {
		t.Fatal("missing db")
	}
}

func TestIsEnvFile(t *testing.T) {
	if !isEnvFile(".env") || !isEnvFile(".env.local") {
		t.Fatal("expected .env files accepted")
	}
	if isEnvFile(".env.example") || isEnvFile(".env.sample") || isEnvFile("docker-compose.yml") {
		t.Fatal("unexpected env file match")
	}
}

func TestUnquoteFirstDefaultPort(t *testing.T) {
	if unquote(`"hi"`) != "hi" || unquote("'x'") != "x" {
		t.Fatal(unquote(`"hi"`), unquote("'x'"))
	}
	if unquote("plain # c") != "plain" {
		t.Fatal(unquote("plain # c"))
	}
	if first(map[string]string{"A": "", "B": "b"}, "A", "B") != "b" {
		t.Fatal("first should skip empty")
	}
	if first(nil, "A") != "" {
		t.Fatal("empty map")
	}
	if defaultPort(config.EngineMySQL) != 3306 || defaultPort(config.EnginePostgres) != 5432 {
		t.Fatal("ports")
	}
}

func TestIsDump(t *testing.T) {
	if !isDump("a.sql") || !isDump("a.DUMP") || !isDump("a.backup") {
		t.Fatal("expected dump")
	}
	if isDump("a.txt") {
		t.Fatal("txt")
	}
}

func TestDumpFilesSkipsVendorAndDotDirs(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("root.sql", "x")
	write("db/app.dump", "x")
	write("vendor/skip.sql", "x")
	write("node_modules/skip.sql", "x")
	write(".git/skip.sql", "x")
	write(".hidden/skip.sql", "x")
	write("nested/deep/skip.sql", "x")
	files, err := dumpFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %#v", files)
	}
}

func TestEngineFromService(t *testing.T) {
	e, ok := engineFromService("postgres:16-alpine", "db", nil)
	if !ok || e != config.EnginePostgres {
		t.Fatal(e, ok)
	}
	e, ok = engineFromService("public.ecr.aws/docker/library/mysql:8", "db", nil)
	if !ok || e != config.EngineMySQL {
		t.Fatal(e, ok)
	}
	e, ok = engineFromService("custom", "app", map[string]string{"POSTGRES_DB": "x"})
	if !ok || e != config.EnginePostgres {
		t.Fatal(e, ok)
	}
	e, ok = engineFromService("custom", "app", map[string]string{"MYSQL_DATABASE": "x"})
	if !ok || e != config.EngineMySQL {
		t.Fatal(e, ok)
	}
	if _, ok := engineFromService("redis", "cache", nil); ok {
		t.Fatal("redis")
	}
	e, ok = engineFromService("", "postgres", nil)
	if !ok || e != config.EnginePostgres {
		t.Fatal("service name fallback")
	}
}

func TestParseComposeEnvAndPorts(t *testing.T) {
	env := parseComposeEnv(map[string]any{"POSTGRES_DB": "epbt", "N": 1, "OK": true})
	if env["POSTGRES_DB"] != "epbt" || env["N"] != "1" || env["OK"] != "true" {
		t.Fatalf("%v", env)
	}
	env = parseComposeEnv([]any{"MYSQL_DATABASE=shop", "MYSQL_USER=app"})
	if env["MYSQL_DATABASE"] != "shop" {
		t.Fatalf("%v", env)
	}
	env = parseComposeEnv(map[any]any{"A": "b"})
	if env["A"] != "b" {
		t.Fatalf("%v", env)
	}

	h, tgt := parsePortMapping("5433:5432/tcp")
	if h != 5433 || tgt != 5432 {
		t.Fatal(h, tgt)
	}
	h, tgt = parsePortMapping("5432")
	if h != 5432 || tgt != 5432 {
		t.Fatal(h, tgt)
	}
	h, tgt = parsePortMapping("127.0.0.1:3307:3306")
	if h != 3307 || tgt != 3306 {
		t.Fatal(h, tgt)
	}
	h, tgt = parsePortMapping(5432)
	if h != 5432 {
		t.Fatal(h)
	}
	h, tgt = parsePortMapping(map[string]any{"published": "5433", "target": 5432})
	if h != 5433 || tgt != 5432 {
		t.Fatal(h, tgt)
	}
	h, tgt = parsePortMapping(map[any]any{"published": 9, "target": 10})
	if h != 9 || tgt != 10 {
		t.Fatal(h, tgt)
	}
	if anyInt(int64(3)) != 3 || anyInt(float64(4)) != 4 || anyInt(true) != 0 {
		t.Fatal("anyInt")
	}
	if stringify(int64(2)) != "2" || stringify(false) != "false" || stringify(struct{}{}) != "" {
		t.Fatal("stringify")
	}

	svc := composeService{Ports: []any{"6379:6379", "5433:5432"}}
	if publishedHostPort(svc, 5432) != 5433 {
		t.Fatal("prefer matching target")
	}
	svc = composeService{Ports: []any{"3307:3306"}}
	if publishedHostPort(svc, 5432) != 3307 {
		t.Fatal("fallback first published")
	}
}

func TestServiceConnectionDefaults(t *testing.T) {
	c, pw, ok := serviceConnection("postgres", composeService{
		Image:       "postgres",
		Environment: map[string]any{"POSTGRES_PASSWORD": "x"},
	}, "compose.yaml")
	if !ok || c.User != "postgres" || c.Database != "postgres" || pw != "x" {
		t.Fatalf("pg default %#v pw=%q ok=%v", c, pw, ok)
	}
	_, _, ok = serviceConnection("db", composeService{
		Image:       "mysql",
		Environment: map[string]any{"MYSQL_ROOT_PASSWORD": "rootpw"},
	}, "compose.yaml")
	if ok {
		t.Fatal("mysql without database should skip")
	}
	c, pw, ok = serviceConnection("db", composeService{
		Image: "mysql",
		Environment: map[string]any{
			"MYSQL_DATABASE":      "shop",
			"MYSQL_ROOT_PASSWORD": "rootpw",
		},
	}, "compose.yaml")
	if !ok || c.User != "root" || pw != "rootpw" {
		t.Fatalf("mysql root %#v pw=%q", c, pw)
	}
}

func TestParseEnvConnectionMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("DB_CONNECTION=sqlite\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := parseEnvConnection(p, ".env"); err != nil || ok {
		t.Fatalf("sqlite should skip ok=%v err=%v", ok, err)
	}
	if _, err := parseEnvFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file")
	}
}
