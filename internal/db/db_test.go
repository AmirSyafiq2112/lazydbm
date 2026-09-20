package db

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

func pgConn() config.Connection {
	return config.Connection{
		Engine: config.EnginePostgres, Host: "127.0.0.1", Port: 5432, User: "app", Database: "epbt",
	}
}

func myConn() config.Connection {
	return config.Connection{
		Engine: config.EngineMySQL, Host: "localhost", Port: 3306, User: "root", Database: "shop",
	}
}

type recorded struct {
	name  string
	args  []string
	stdin string
}

type recorder struct {
	calls  []recorded
	err    error
	output []string
}

func (r *recorder) run(_ context.Context, _ string, log LogFunc, name string, args []string, stdin io.Reader) error {
	var body []byte
	if stdin != nil {
		body, _ = io.ReadAll(stdin)
	}
	r.calls = append(r.calls, recorded{name: name, args: append([]string{}, args...), stdin: string(body)})
	if log != nil {
		log("$ " + name + " " + strings.Join(args, " "))
		for _, line := range r.output {
			log(line)
		}
	}
	return r.err
}

func withFakeExec(t *testing.T, missing map[string]bool) *recorder {
	t.Helper()
	rec := &recorder{}
	origL, origR := lookPath, commandRunner
	t.Cleanup(func() {
		lookPath = origL
		commandRunner = origR
	})
	lookPath = func(bin string) (string, error) {
		if missing[bin] {
			return "", errors.New("not found")
		}
		return "/bin/" + bin, nil
	}
	commandRunner = rec.run
	return rec
}

func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent(`foo"bar`); got != `"foo""bar"` {
		t.Fatalf("quoteIdent = %s", got)
	}
}

func TestQuoteLiteral(t *testing.T) {
	if got := quoteLiteral(`o'reilly`); got != `'o''reilly'` {
		t.Fatalf("quoteLiteral = %s", got)
	}
}

func TestQuoteMySQL(t *testing.T) {
	if got := quoteMySQL("a`b"); got != "`a``b`" {
		t.Fatalf("quoteMySQL = %s", got)
	}
}

func TestIsCustomDump(t *testing.T) {
	if !IsCustomDump("backup.dump") || !IsCustomDump("x.backup") || !IsCustomDump("a.pgdump") {
		t.Fatal("expected custom dump")
	}
	if IsCustomDump("schema.sql") {
		t.Fatal("sql is not a custom dump")
	}
}

func TestRequiredBins(t *testing.T) {
	c := pgConn()
	if got := strings.Join(requiredBins(c, "a.sql", false, false), ","); got != "psql" {
		t.Fatalf("import sql = %s", got)
	}
	if got := strings.Join(requiredBins(c, "a.dump", false, false), ","); got != "pg_restore,psql" {
		t.Fatalf("import dump = %s", got)
	}
	if got := strings.Join(requiredBins(c, "a.dump", true, false), ","); got != "pg_restore,psql" {
		t.Fatalf("clear dump = %s", got)
	}
	if got := strings.Join(requiredBins(c, "", false, true), ","); got != "pg_dump" {
		t.Fatalf("export = %s", got)
	}
	m := myConn()
	if got := strings.Join(requiredBins(m, "a.sql", true, false), ","); got != "mysql" {
		t.Fatalf("mysql import = %s", got)
	}
	if got := strings.Join(requiredBins(m, "", false, true), ","); got != "mysqldump" {
		t.Fatalf("mysql export = %s", got)
	}
}

func TestMissingTools(t *testing.T) {
	withFakeExec(t, map[string]bool{"psql": true})
	got := MissingTools(pgConn(), "a.sql", false, false)
	if strings.Join(got, ",") != "psql" {
		t.Fatalf("missing = %v", got)
	}
	withFakeExec(t, nil)
	if len(MissingTools(pgConn(), "a.sql", false, false)) != 0 {
		t.Fatal("expected none missing")
	}
}

func TestPsqlAndPgConnArgs(t *testing.T) {
	c := pgConn()
	args := psqlArgs(c, "postgres")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-d postgres") || !strings.Contains(joined, "-U app") {
		t.Fatalf("args = %v", args)
	}
	pg := pgConnArgs(c, c.Database)
	if strings.Join(pg[len(pg)-2:], " ") != "-d epbt" {
		t.Fatalf("pgConnArgs = %v", pg)
	}
}

func TestMysqlArgsNoPasswordFlag(t *testing.T) {
	c := myConn()
	args := mysqlArgs(c, "shop")
	for _, a := range args {
		if strings.HasPrefix(a, "-p") {
			t.Fatalf("password must not appear in args: %v", args)
		}
	}
	if mysqlArgs(c, "")[len(mysqlArgs(c, ""))-1] == "shop" {
		t.Fatal("empty database should omit db name")
	}
	args = mysqlArgs(c, "shop")
	if !contains(args, "--") || args[len(args)-1] != "shop" || args[len(args)-2] != "--" {
		t.Fatalf("database must follow -- : %v", args)
	}
}

func TestSafeCLIFileArg(t *testing.T) {
	if got := safeCLIFileArg("-f.sql"); got != "./-f.sql" {
		t.Fatalf("got %q", got)
	}
	if got := safeCLIFileArg("./-f.sql"); got != "./-f.sql" {
		t.Fatalf("got %q", got)
	}
	if got := safeCLIFileArg("dump.sql"); got != "dump.sql" {
		t.Fatalf("got %q", got)
	}
}

func writeTempSQL(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportDashFilename(t *testing.T) {
	rec := withFakeExec(t, nil)
	sql := writeTempSQL(t, "-f.sql", "SELECT 1;\nALTER TABLE t OWNER TO epbt_app;\n")
	if err := Import(context.Background(), pgConn(), "", sql, false, func(string) {}); err != nil {
		t.Fatal(err)
	}
	args := rec.calls[len(rec.calls)-1].args
	if contains(args, "-f") || contains(args, "-f.sql") || contains(args, sql) {
		t.Fatalf("sql import must use stdin, not -f: %v", args)
	}
	if strings.Contains(rec.calls[len(rec.calls)-1].stdin, "OWNER TO") {
		t.Fatalf("owners should be stripped: %q", rec.calls[len(rec.calls)-1].stdin)
	}
	if !strings.Contains(rec.calls[len(rec.calls)-1].stdin, "SELECT 1") {
		t.Fatalf("sql body missing: %q", rec.calls[len(rec.calls)-1].stdin)
	}

	rec = withFakeExec(t, nil)
	if err := Import(context.Background(), pgConn(), "", "-x.dump", false, func(string) {}); err != nil {
		t.Fatal(err)
	}
	args = rec.calls[len(rec.calls)-1].args
	if contains(args, "-x.dump") {
		t.Fatalf("raw dump name: %v", args)
	}
	if !contains(args, "./-x.dump") || !contains(args, "--") {
		t.Fatalf("pg_restore args = %v", args)
	}
}

func TestRedactAndArgs(t *testing.T) {
	got := redact("password=super-secret in error", "super-secret")
	if strings.Contains(got, "super-secret") {
		t.Fatalf("password leaked: %q", got)
	}
	if redact("plain", "") != "plain" {
		t.Fatal("empty password should not change text")
	}
	in := []string{"-h", "localhost"}
	out := redactArgs(in)
	in[0] = "changed"
	if out[0] != "-h" {
		t.Fatal("redactArgs should copy")
	}
}

func TestWithPassword(t *testing.T) {
	env := withPassword([]string{"PATH=/bin", "PGPASSWORD=old"}, "new", "psql")
	found := false
	for _, e := range env {
		if e == "PGPASSWORD=new" {
			found = true
		}
		if e == "PGPASSWORD=old" {
			t.Fatal("old password env should be replaced")
		}
	}
	if !found {
		t.Fatalf("env = %v", env)
	}
	env = withPassword(nil, "x", "mysql")
	if env[len(env)-1] != "MYSQL_PWD=x" {
		t.Fatalf("mysql env = %v", env)
	}
}

func TestResetSQL(t *testing.T) {
	pg := postgresResetSQL(config.Connection{User: `a"b`, Database: `o'reilly`})
	if !strings.Contains(pg, `WHERE datname = 'o''reilly'`) {
		t.Fatalf("literal: %s", pg)
	}
	if !strings.Contains(pg, `DROP DATABASE IF EXISTS "o'reilly"`) {
		t.Fatalf("ident: %s", pg)
	}
	if !strings.Contains(pg, `OWNER "a""b"`) {
		t.Fatalf("owner: %s", pg)
	}
	my := mysqlResetSQL(config.Connection{Database: "a`b"})
	if my != "DROP DATABASE IF EXISTS `a``b`; CREATE DATABASE `a``b`;" {
		t.Fatalf("mysql sql = %s", my)
	}
}

func TestEnsureDatabaseCreatesWhenMissing(t *testing.T) {
	rec := withFakeExec(t, nil)
	var logs []string
	if err := EnsureDatabase(context.Background(), pgConn(), "", func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("calls = %#v", rec.calls)
	}
	if !strings.Contains(rec.calls[0].args[len(rec.calls[0].args)-1], "pg_database") {
		t.Fatalf("exists query = %v", rec.calls[0].args)
	}
	if !strings.Contains(rec.calls[1].stdin, `CREATE DATABASE "epbt"`) {
		t.Fatalf("create stdin = %s", rec.calls[1].stdin)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "creating database epbt") {
		t.Fatalf("logs = %s", joined)
	}

	rec = withFakeExec(t, nil)
	if err := EnsureDatabase(context.Background(), myConn(), "", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.calls[len(rec.calls)-1].stdin, "CREATE DATABASE IF NOT EXISTS `shop`") {
		t.Fatalf("mysql create = %#v", rec.calls)
	}
}

func TestEnsureDatabaseSkipsWhenExists(t *testing.T) {
	rec := withFakeExec(t, nil)
	rec.output = []string{"1"}
	var logs []string
	if err := EnsureDatabase(context.Background(), pgConn(), "", func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("should not create: %#v", rec.calls)
	}
	if strings.Contains(rec.calls[0].stdin, "CREATE DATABASE") {
		t.Fatalf("exists check should be a query: %#v", rec.calls[0])
	}
	if !strings.Contains(strings.Join(logs, "\n"), "already exists") {
		t.Fatalf("logs = %s", strings.Join(logs, "\n"))
	}
}

func TestImportCreatesMissingDatabase(t *testing.T) {
	rec := withFakeExec(t, nil)
	file := writeTempSQL(t, "dump.sql", "SELECT 1;\nALTER TABLE t OWNER TO epbt_app;\n")
	var logs []string
	if err := Import(context.Background(), pgConn(), "", file, false, func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 3 {
		t.Fatalf("exists + create + import, got %#v", rec.calls)
	}
	if !strings.Contains(rec.calls[1].stdin, `CREATE DATABASE "epbt"`) {
		t.Fatalf("create = %s", rec.calls[1].stdin)
	}
	imp := rec.calls[2]
	if contains(imp.args, "-f") {
		t.Fatalf("sql import must use stdin: %v", imp.args)
	}
	if strings.Contains(imp.stdin, "OWNER TO") || !strings.Contains(imp.stdin, "SELECT 1") {
		t.Fatalf("filtered stdin = %q", imp.stdin)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "creating database") || strings.Contains(joined, "clearing database") {
		t.Fatalf("logs = %s", joined)
	}
	if !strings.Contains(joined, "owners/grants skipped") {
		t.Fatalf("logs = %s", joined)
	}
}

func TestImportValidation(t *testing.T) {
	ctx := context.Background()
	log := func(string) {}
	if err := Import(ctx, pgConn(), "", "", false, log); err == nil {
		t.Fatal("empty file")
	}
	if err := Import(ctx, config.Connection{}, "a.sql", "x", false, log); err == nil {
		t.Fatal("invalid conn")
	}
	if err := Import(ctx, myConn(), "pw", "x.dump", false, log); err == nil {
		t.Fatal("mysql custom dump")
	}
}

func TestImportPostgresSQLAndClear(t *testing.T) {
	rec := withFakeExec(t, nil)
	file := writeTempSQL(t, "dump.sql", "SELECT 1;\nGRANT ALL ON SCHEMA public TO epbt_app;\n")
	var logs []string
	err := Import(context.Background(), pgConn(), "secret", file, true, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("calls = %#v", rec.calls)
	}
	if rec.calls[0].name != "psql" || rec.calls[1].name != "psql" {
		t.Fatalf("bins = %#v", rec.calls)
	}
	if !strings.Contains(rec.calls[0].stdin, "DROP DATABASE") {
		t.Fatalf("reset stdin = %s", rec.calls[0].stdin)
	}
	if contains(rec.calls[1].args, "-f") {
		t.Fatalf("sql import must use stdin: %v", rec.calls[1].args)
	}
	if strings.Contains(rec.calls[1].stdin, "GRANT ALL") || !strings.Contains(rec.calls[1].stdin, "SELECT 1") {
		t.Fatalf("filtered stdin = %q", rec.calls[1].stdin)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "clearing database") || strings.Contains(joined, "secret") {
		t.Fatalf("logs = %s", joined)
	}
	if !strings.Contains(joined, "owners/grants skipped") {
		t.Fatalf("logs = %s", joined)
	}
}

func TestImportPostgresCustomDump(t *testing.T) {
	rec := withFakeExec(t, nil)
	if err := Import(context.Background(), pgConn(), "", "db.backup", false, func(string) {}); err != nil {
		t.Fatal(err)
	}
	last := rec.calls[len(rec.calls)-1]
	if last.name != "pg_restore" || !contains(last.args, "db.backup") {
		t.Fatalf("calls = %#v", rec.calls)
	}
}

func TestImportMySQL(t *testing.T) {
	rec := withFakeExec(t, nil)
	dir := t.TempDir()
	file := filepath.Join(dir, "shop.sql")
	if err := os.WriteFile(file, []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Import(context.Background(), myConn(), "pw", file, true, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 2 || rec.calls[0].name != "mysql" || rec.calls[1].name != "mysql" {
		t.Fatalf("calls = %#v", rec.calls)
	}
	if rec.calls[1].stdin != "SELECT 1;" {
		t.Fatalf("stdin = %q", rec.calls[1].stdin)
	}
}

func TestImportMissingTools(t *testing.T) {
	withFakeExec(t, map[string]bool{"psql": true})
	err := Import(context.Background(), pgConn(), "", "a.sql", false, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "psql") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportUnsupportedEngine(t *testing.T) {
	withFakeExec(t, nil)
	c := pgConn()
	c.Engine = "oracle"
	if err := Import(context.Background(), c, "", "a.sql", false, func(string) {}); err == nil {
		t.Fatal("expected unsupported")
	}
}

func TestExportPostgresAndMySQL(t *testing.T) {
	rec := withFakeExec(t, nil)
	dir := t.TempDir()
	out := filepath.Join(dir, "sub", "epbt.sql")
	if err := Export(context.Background(), pgConn(), "pw", out, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if rec.calls[0].name != "pg_dump" || !contains(rec.calls[0].args, "--no-owner") {
		t.Fatalf("pg_dump args = %v", rec.calls[0].args)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}

	rec = withFakeExec(t, nil)
	out = filepath.Join(dir, "shop.sql")
	if err := Export(context.Background(), myConn(), "pw", out, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if rec.calls[0].name != "mysqldump" || !contains(rec.calls[0].args, "--single-transaction") {
		t.Fatalf("mysqldump args = %v", rec.calls[0].args)
	}
}

func TestExportValidation(t *testing.T) {
	if err := Export(context.Background(), pgConn(), "", "", func(string) {}); err == nil {
		t.Fatal("empty path")
	}
	if err := Export(context.Background(), config.Connection{}, "", "x.sql", func(string) {}); err == nil {
		t.Fatal("invalid conn")
	}
	withFakeExec(t, map[string]bool{"pg_dump": true})
	if err := Export(context.Background(), pgConn(), "", filepath.Join(t.TempDir(), "x.sql"), func(string) {}); err == nil {
		t.Fatal("missing pg_dump")
	}
}

func TestResetUnsupported(t *testing.T) {
	c := pgConn()
	c.Engine = "oracle"
	if err := reset(context.Background(), c, "", func(string) {}); err == nil {
		t.Fatal("expected error")
	}
}

func TestStreamRedactsPassword(t *testing.T) {
	var lines []string
	err := stream(bytes.NewBufferString("fail: hunter2\nok\n"), "hunter2", func(s string) {
		lines = append(lines, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if lines[0] != "fail: ********" || lines[1] != "ok" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestRunUsesEcho(t *testing.T) {
	if _, err := exec.LookPath("echo"); err != nil {
		t.Skip("echo not on PATH")
	}
	var lines []string
	err := run(context.Background(), "secret", func(s string) { lines = append(lines, s) }, "echo", []string{"hello-secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "hello-********") {
		t.Fatalf("logs = %s", joined)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestListDatabasesPostgresAndMySQL(t *testing.T) {
	origL, origR := lookPath, commandRunner
	t.Cleanup(func() { lookPath = origL; commandRunner = origR })
	lookPath = func(bin string) (string, error) { return "/bin/" + bin, nil }
	var gotName string
	var gotArgs []string
	commandRunner = func(_ context.Context, _ string, log LogFunc, name string, args []string, _ io.Reader) error {
		gotName, gotArgs = name, append([]string{}, args...)
		if log != nil {
			log("$ " + name + " " + strings.Join(args, " "))
			log("epbt")
			log("postgres")
		}
		return nil
	}
	names, err := ListDatabases(context.Background(), pgConn(), "pw", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "psql" || !contains(gotArgs, "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY 1;") {
		t.Fatalf("psql args = %s %v", gotName, gotArgs)
	}
	if !contains(gotArgs, "postgres") {
		t.Fatalf("must use maintenance db postgres: %v", gotArgs)
	}
	if strings.Join(names, ",") != "epbt,postgres" {
		t.Fatalf("names = %v", names)
	}

	commandRunner = func(_ context.Context, _ string, log LogFunc, name string, args []string, _ io.Reader) error {
		gotName, gotArgs = name, append([]string{}, args...)
		if log != nil {
			log("shop")
			log("mysql")
		}
		return nil
	}
	names, err = ListDatabases(context.Background(), myConn(), "pw", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "mysql" || !contains(gotArgs, "SHOW DATABASES") {
		t.Fatalf("mysql args = %s %v", gotName, gotArgs)
	}
	if contains(gotArgs, "shop") {
		t.Fatalf("SHOW DATABASES must not select a database: %v", gotArgs)
	}
	if strings.Join(names, ",") != "mysql,shop" {
		t.Fatalf("names = %v", names)
	}

	commandRunner = func(_ context.Context, _ string, log LogFunc, _ string, _ []string, _ io.Reader) error {
		if log != nil {
			log("ok")
			log("../etc")
			log("-bad")
			log("a/b")
		}
		return nil
	}
	names, err = ListDatabases(context.Background(), pgConn(), "pw", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "ok" {
		t.Fatalf("invalid names must be skipped: %v", names)
	}
}

func TestListDatabasesMissingToolsAndValidation(t *testing.T) {
	withFakeExec(t, map[string]bool{"psql": true})
	if _, err := ListDatabases(context.Background(), pgConn(), "", func(string) {}); err == nil || !strings.Contains(err.Error(), "psql") {
		t.Fatalf("err = %v", err)
	}
	evil := pgConn()
	evil.User = "--evil"
	withFakeExec(t, nil)
	if _, err := ListDatabases(context.Background(), evil, "", func(string) {}); err == nil {
		t.Fatal("ValidateFields must reject --evil")
	}
}

func TestTestConnectionPostgresAndMySQL(t *testing.T) {
	origL, origR := lookPath, commandRunner
	t.Cleanup(func() { lookPath = origL; commandRunner = origR })
	lookPath = func(bin string) (string, error) { return "/bin/" + bin, nil }
	var gotName string
	var gotArgs []string
	commandRunner = func(_ context.Context, _ string, log LogFunc, name string, args []string, _ io.Reader) error {
		gotName, gotArgs = name, append([]string{}, args...)
		if log != nil {
			log("$ " + name)
			log("1")
		}
		return nil
	}
	if err := TestConnection(context.Background(), pgConn(), "pw", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if gotName != "psql" || !contains(gotArgs, "SELECT 1") || !contains(gotArgs, "epbt") {
		t.Fatalf("psql test args = %s %v", gotName, gotArgs)
	}

	commandRunner = func(_ context.Context, _ string, log LogFunc, name string, args []string, _ io.Reader) error {
		gotName, gotArgs = name, append([]string{}, args...)
		return nil
	}
	if err := TestConnection(context.Background(), myConn(), "pw", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if gotName != "mysql" || !contains(gotArgs, "SELECT 1") {
		t.Fatalf("mysql test args = %s %v", gotName, gotArgs)
	}

	server := myConn()
	server.Database = ""
	if err := TestConnection(context.Background(), server, "pw", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if gotName != "mysql" || !contains(gotArgs, "SELECT 1") || !contains(gotArgs, "mysql") {
		t.Fatalf("mysql maintenance db args = %s %v", gotName, gotArgs)
	}

	pgServer := pgConn()
	pgServer.Database = ""
	if err := TestConnection(context.Background(), pgServer, "pw", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !contains(gotArgs, "postgres") {
		t.Fatalf("postgres maintenance db args = %v", gotArgs)
	}

	commandRunner = func(_ context.Context, _ string, _ LogFunc, _ string, _ []string, _ io.Reader) error {
		return errors.New("connection refused")
	}
	if err := TestConnection(context.Background(), pgConn(), "pw", func(string) {}); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v", err)
	}
}
