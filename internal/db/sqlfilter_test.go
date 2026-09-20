package db

import (
	"strings"
	"testing"
)

func filterSQL(t *testing.T, in string) string {
	t.Helper()
	var buf strings.Builder
	if err := writeFilteredPostgresSQL(&buf, strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestStripOwnersAndGrants(t *testing.T) {
	in := strings.Join([]string{
		`SET statement_timeout = 0;`,
		`SELECT pg_catalog.set_config('search_path', '', false);`,
		`CREATE SCHEMA app;`,
		`ALTER SCHEMA app OWNER TO epbt_app;`,
		`GRANT ALL ON SCHEMA app TO epbt_app;`,
		`REVOKE ALL ON SCHEMA app FROM PUBLIC;`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE epbt_app GRANT SELECT ON TABLES TO PUBLIC;`,
		`SET ROLE epbt_app;`,
		`SET SESSION AUTHORIZATION epbt_app;`,
		`CREATE TABLE app.t (id int);`,
		`ALTER TABLE app.t OWNER TO epbt_app;`,
		``,
	}, "\n")
	got := filterSQL(t, in)
	for _, drop := range []string{
		"OWNER TO",
		"GRANT ALL",
		"REVOKE ALL",
		"ALTER DEFAULT PRIVILEGES",
		"SET ROLE",
		"SET SESSION AUTHORIZATION",
	} {
		if strings.Contains(got, drop) {
			t.Fatalf("should drop %q, got:\n%s", drop, got)
		}
	}
	for _, keep := range []string{
		"SET statement_timeout = 0;",
		"set_config",
		"CREATE SCHEMA app;",
		"CREATE TABLE app.t (id int);",
	} {
		if !strings.Contains(got, keep) {
			t.Fatalf("should keep %q, got:\n%s", keep, got)
		}
	}
}

func TestStripOwnersKeepsDollarQuotedFunction(t *testing.T) {
	in := strings.Join([]string{
		`CREATE FUNCTION app.f() RETURNS void LANGUAGE plpgsql AS $body$`,
		`BEGIN`,
		`  -- pretend: ALTER TABLE t OWNER TO epbt_app;`,
		`  RAISE NOTICE 'OWNER TO epbt_app';`,
		`END;`,
		`$body$;`,
		`ALTER FUNCTION app.f() OWNER TO epbt_app;`,
		``,
	}, "\n")
	got := filterSQL(t, in)
	if !strings.Contains(got, "RAISE NOTICE 'OWNER TO epbt_app'") {
		t.Fatalf("function body lost:\n%s", got)
	}
	if !strings.Contains(got, "$body$") {
		t.Fatalf("dollar quotes lost:\n%s", got)
	}
	if strings.Count(got, "OWNER TO") != 2 {
		t.Fatalf("body OWNER TO should remain, ALTER should drop:\n%s", got)
	}
	if strings.Contains(got, "ALTER FUNCTION") {
		t.Fatalf("ALTER FUNCTION OWNER should drop:\n%s", got)
	}
}

func TestRewriteCreateSchemaAuthorization(t *testing.T) {
	got := filterSQL(t, `CREATE SCHEMA foo AUTHORIZATION epbt_app;`)
	if strings.Contains(got, "AUTHORIZATION") {
		t.Fatalf("authorization left behind: %q", got)
	}
	if !strings.Contains(got, "CREATE SCHEMA foo") {
		t.Fatalf("schema missing: %q", got)
	}

	got = filterSQL(t, `CREATE SCHEMA AUTHORIZATION epbt_app;`)
	if strings.Contains(got, "AUTHORIZATION") {
		t.Fatalf("auth-only left behind: %q", got)
	}
	if !strings.Contains(got, `CREATE SCHEMA epbt_app`) {
		t.Fatalf("auth-only should keep role as schema name: %q", got)
	}

	got = filterSQL(t, `-- owner comment
CREATE SCHEMA IF NOT EXISTS foo AUTHORIZATION "epbt_app";`)
	if strings.Contains(got, "AUTHORIZATION") {
		t.Fatalf("quoted auth left behind: %q", got)
	}
	if !strings.Contains(got, "CREATE SCHEMA IF NOT EXISTS foo") {
		t.Fatalf("if not exists lost: %q", got)
	}
}

func TestFilterKeepsCopyDataAndMeta(t *testing.T) {
	in := strings.Join([]string{
		`\restrict secretkey`,
		`COPY public.t (id, name) FROM stdin;`,
		`1	GRANT ALL;`,
		`2	OWNER TO epbt_app`,
		`\.`,
		`ALTER TABLE public.t OWNER TO epbt_app;`,
		``,
	}, "\n")
	got := filterSQL(t, in)
	if !strings.Contains(got, `\restrict secretkey`) {
		t.Fatalf("meta lost:\n%s", got)
	}
	if !strings.Contains(got, "1\tGRANT ALL;") || !strings.Contains(got, "2\tOWNER TO epbt_app") {
		t.Fatalf("copy rows lost:\n%s", got)
	}
	if !strings.Contains(got, `\.`) {
		t.Fatalf("copy end lost:\n%s", got)
	}
	if strings.Contains(got, "ALTER TABLE") {
		t.Fatalf("owner alter should drop:\n%s", got)
	}
}

func TestStripWrappedPrivilegeStatements(t *testing.T) {
	in := strings.Join([]string{
		`(GRANT ALL ON SCHEMA public TO attacker_role);`,
		`((REVOKE ALL ON SCHEMA public FROM PUBLIC));`,
		`WITH x AS (SELECT 1) GRANT ALL ON DATABASE app TO attacker_role;`,
		`(ALTER TABLE t OWNER TO attacker_role);`,
		`CREATE TABLE app.t (id int);`,
		``,
	}, "\n")
	got := filterSQL(t, in)
	for _, drop := range []string{"GRANT ALL", "REVOKE ALL", "OWNER TO", "WITH x"} {
		if strings.Contains(got, drop) {
			t.Fatalf("wrapped privilege should drop %q, got:\n%s", drop, got)
		}
	}
	if !strings.Contains(got, "CREATE TABLE app.t (id int);") {
		t.Fatalf("table should remain:\n%s", got)
	}
}

func TestFilterKeepsQuotedSemicolon(t *testing.T) {
	in := `INSERT INTO t VALUES ('GRANT ALL ON x TO y;');` + "\n" + `GRANT ALL ON TABLE t TO epbt_app;` + "\n"
	got := filterSQL(t, in)
	if !strings.Contains(got, `'GRANT ALL ON x TO y;'`) {
		t.Fatalf("insert lost:\n%s", got)
	}
	if strings.Contains(got, "GRANT ALL ON TABLE") {
		t.Fatalf("real grant should drop:\n%s", got)
	}
}

func TestFilterEmptyDollarBody(t *testing.T) {
	in := "SELECT $$$$ AS empty;\nALTER TABLE t OWNER TO x;\n"
	got := filterSQL(t, in)
	if !strings.Contains(got, "$$$$") {
		t.Fatalf("empty dollar quote lost: %q", got)
	}
	if strings.Contains(got, "OWNER TO") {
		t.Fatalf("owner should drop: %q", got)
	}
}
