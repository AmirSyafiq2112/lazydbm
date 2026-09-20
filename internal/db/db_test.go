package db

import (
	"strings"
	"testing"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

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
	if !IsCustomDump("backup.dump") || !IsCustomDump("x.backup") {
		t.Fatal("expected custom dump")
	}
	if IsCustomDump("schema.sql") {
		t.Fatal("sql is not a custom dump")
	}
}

func TestPsqlArgs(t *testing.T) {
	c := config.Connection{Host: "127.0.0.1", Port: 5432, User: "app", Database: "epbt"}
	args := psqlArgs(c, "postgres")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-d postgres") || !strings.Contains(joined, "-U app") {
		t.Fatalf("args = %v", args)
	}
}

func TestMysqlArgsNoPasswordFlag(t *testing.T) {
	c := config.Connection{Host: "localhost", Port: 3306, User: "root"}
	args := mysqlArgs(c, "shop")
	for _, a := range args {
		if strings.HasPrefix(a, "-p") {
			t.Fatalf("password must not appear in args: %v", args)
		}
	}
}

func TestRedact(t *testing.T) {
	got := redact("password=super-secret in error", "super-secret")
	if strings.Contains(got, "super-secret") {
		t.Fatalf("password leaked: %q", got)
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
