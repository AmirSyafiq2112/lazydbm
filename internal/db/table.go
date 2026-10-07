package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

// TableRef is a table named by a dump or an export preview.
type TableRef struct {
	Schema string
	Name   string
}

func (t TableRef) Label() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// TableStat is one table in an export preview.
type TableStat struct {
	Schema string
	Name   string
	Bytes  int64
}

func (t TableStat) Label() string {
	return (TableRef{Schema: t.Schema, Name: t.Name}).Label()
}

// ListTables returns base tables. Postgres lists tables in schema. MySQL lists tables in c.Database and ignores schema.
func ListTables(ctx context.Context, c config.Connection, password, schema string, log LogFunc) ([]string, error) {
	if err := requireCreds(c); err != nil {
		return nil, err
	}
	if c.Database == "" {
		return nil, fmt.Errorf("database name required")
	}
	switch c.Engine {
	case config.EnginePostgres:
		schema = strings.TrimSpace(schema)
		if err := config.ValidateSchema(schema); err != nil {
			return nil, err
		}
		if _, err := lookPath("psql"); err != nil {
			return nil, fmt.Errorf("missing client tools: psql")
		}
		query := fmt.Sprintf(`
SELECT c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = %s
  AND c.relkind IN ('r', 'p')
  AND NOT c.relispartition
ORDER BY 1;`, quoteLiteral(schema))
		args := append(psqlArgs(c, c.Database), "-X", "-w", "-tA", "-c", query)
		lines, err := collectQuery(ctx, password, log, "psql", args)
		if err != nil {
			return nil, err
		}
		return filterTableNames(lines), nil
	case config.EngineMySQL:
		if _, err := lookPath("mysql"); err != nil {
			return nil, fmt.Errorf("missing client tools: mysql")
		}
		query := fmt.Sprintf(`SELECT table_name FROM information_schema.tables WHERE table_schema = %s AND table_type = 'BASE TABLE' ORDER BY 1`, quoteLiteral(c.Database))
		args := append(mysqlArgs(c, ""), "-N", "-e", query)
		lines, err := collectQuery(ctx, password, log, "mysql", args)
		if err != nil {
			return nil, err
		}
		return filterTableNames(lines), nil
	default:
		return nil, fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

func filterTableNames(lines []string) []string {
	names := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if i := strings.IndexByte(name, '\t'); i >= 0 {
			name = name[:i]
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if err := config.ValidateTable(name); err != nil {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// PreviewTableExport lists the selected table and every table it reaches through foreign keys, with sizes.
func PreviewTableExport(ctx context.Context, c config.Connection, password, schema, table string, log LogFunc) ([]TableStat, error) {
	table = strings.TrimSpace(table)
	if err := config.ValidateTable(table); err != nil {
		return nil, err
	}
	if err := requireCreds(c); err != nil {
		return nil, err
	}
	if c.Database == "" {
		return nil, fmt.Errorf("database name required")
	}
	var lines []string
	var err error
	switch c.Engine {
	case config.EnginePostgres:
		schema = strings.TrimSpace(schema)
		if err = config.ValidateSchema(schema); err != nil {
			return nil, err
		}
		if _, err = lookPath("psql"); err != nil {
			return nil, fmt.Errorf("missing client tools: psql")
		}
		args := append(psqlArgs(c, c.Database), "-X", "-w", "-tA", "-F", "|", "-c", tablePreviewSQL(schema, table))
		lines, err = collectQuery(ctx, password, log, "psql", args)
	case config.EngineMySQL:
		if _, err = lookPath("mysql"); err != nil {
			return nil, fmt.Errorf("missing client tools: mysql")
		}
		args := append(mysqlArgs(c, ""), "-N", "-e", mysqlTablePreviewSQL(c.Database, table))
		lines, err = collectQuery(ctx, password, log, "mysql", args)
		schema = c.Database
	default:
		return nil, fmt.Errorf("unsupported engine %s", c.Engine)
	}
	if err != nil {
		return nil, err
	}
	stats := make([]TableStat, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected table preview row %q", line)
		}
		item := TableStat{
			Schema: strings.TrimSpace(parts[0]),
			Name:   strings.TrimSpace(parts[1]),
		}
		if err := config.ValidateTable(item.Name); err != nil {
			continue
		}
		if item.Schema != "" {
			if err := config.ValidateSchema(item.Schema); err != nil {
				continue
			}
		}
		bytes, err := parseInt64(strings.TrimSpace(parts[2]))
		if err != nil {
			return nil, fmt.Errorf("table size for %s: %w", item.Label(), err)
		}
		item.Bytes = bytes
		stats = append(stats, item)
	}
	found := false
	for _, s := range stats {
		if s.Name == table && (schema == "" || s.Schema == schema) {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("table %s not found", table)
	}
	return orderTableStats(schema, table, stats), nil
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}

func tablePreviewSQL(schema, table string) string {
	return fmt.Sprintf(`
WITH RECURSIVE reach(nspname, relname) AS (
  SELECT n.nspname, c.relname
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = %s AND c.relname = %s
  UNION
  SELECT fn.nspname, f.relname
  FROM reach r
  JOIN pg_namespace n ON n.nspname = r.nspname
  JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = r.relname
  JOIN pg_constraint con ON con.conrelid = c.oid AND con.contype = 'f'
  JOIN pg_class f ON f.oid = con.confrelid
  JOIN pg_namespace fn ON fn.oid = f.relnamespace
  WHERE fn.nspname <> 'information_schema'
    AND fn.nspname <> 'pg_catalog'
    AND fn.nspname <> 'pg_toast'
    AND strpos(fn.nspname, 'pg_temp_') <> 1
    AND strpos(fn.nspname, 'pg_toast_temp_') <> 1
)
SELECT r.nspname, r.relname, pg_total_relation_size(c.oid)::bigint
FROM reach r
JOIN pg_namespace n ON n.nspname = r.nspname
JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = r.relname;`, quoteLiteral(schema), quoteLiteral(table))
}

func mysqlTablePreviewSQL(database, table string) string {
	return fmt.Sprintf(`
WITH RECURSIVE reach AS (
  SELECT table_schema, table_name
  FROM information_schema.tables
  WHERE table_schema = %s AND table_name = %s
  UNION
  SELECT k.referenced_table_schema, k.referenced_table_name
  FROM reach r
  JOIN information_schema.key_column_usage k
    ON k.table_schema = r.table_schema AND k.table_name = r.table_name
   AND k.referenced_table_name IS NOT NULL
)
SELECT CONCAT(r.table_schema, '|', r.table_name, '|',
  COALESCE((SELECT data_length + index_length FROM information_schema.tables t
            WHERE t.table_schema = r.table_schema AND t.table_name = r.table_name), 0))
FROM reach r;`, quoteLiteral(database), quoteLiteral(table))
}

func orderTableStats(schema, table string, stats []TableStat) []TableStat {
	var head *TableStat
	rest := make([]TableStat, 0, len(stats))
	for _, s := range stats {
		if s.Name == table && (schema == "" || s.Schema == schema) && head == nil {
			cp := s
			head = &cp
			continue
		}
		rest = append(rest, s)
	}
	for i := 1; i < len(rest); i++ {
		j := i
		for j > 0 && (rest[j].Bytes > rest[j-1].Bytes || (rest[j].Bytes == rest[j-1].Bytes && rest[j].Label() < rest[j-1].Label())) {
			rest[j], rest[j-1] = rest[j-1], rest[j]
			j--
		}
	}
	if head == nil {
		return rest
	}
	return append([]TableStat{*head}, rest...)
}

// ExportTables dumps the given tables in one file.
func ExportTables(ctx context.Context, c config.Connection, password string, tables []TableStat, out string, log LogFunc) error {
	if len(tables) == 0 {
		return fmt.Errorf("no table selected")
	}
	if out == "" {
		return fmt.Errorf("no export path")
	}
	if !c.Valid() {
		return fmt.Errorf("incomplete connection")
	}
	if missing := MissingTools(c, "", false, true); len(missing) > 0 {
		return fmt.Errorf("missing client tools: %s", strings.Join(missing, ", "))
	}
	cleaned := make([]TableStat, 0, len(tables))
	seen := map[string]struct{}{}
	for _, t := range tables {
		t.Name = strings.TrimSpace(t.Name)
		t.Schema = strings.TrimSpace(t.Schema)
		if err := config.ValidateTable(t.Name); err != nil {
			return err
		}
		if t.Schema != "" {
			if err := config.ValidateSchema(t.Schema); err != nil {
				return err
			}
		}
		key := t.Label()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, t)
	}
	labels := make([]string, len(cleaned))
	for i, t := range cleaned {
		labels[i] = t.Label()
	}
	log(fmt.Sprintf("exporting %d tables from %s -> %s", len(cleaned), c.Database, out))
	log(strings.Join(labels, ", "))
	if err := touchExport(out); err != nil {
		return err
	}
	switch c.Engine {
	case config.EnginePostgres:
		args := append(pgConnArgs(c, c.Database), "--no-owner", "--no-acl")
		for _, t := range cleaned {
			schema := t.Schema
			if schema == "" {
				schema = "public"
			}
			args = append(args, "--table", quoteIdent(schema)+"."+quoteIdent(t.Name))
		}
		args = append(args, "--file="+out)
		return commandRunner(ctx, password, log, "pg_dump", args, nil)
	case config.EngineMySQL:
		args := mysqlArgs(c, "")
		args = append(args, "--single-transaction", "--result-file="+out, "--", c.Database)
		for _, t := range cleaned {
			if t.Schema != "" && t.Schema != c.Database {
				log(t.Label() + " is outside this database")
				continue
			}
			args = append(args, t.Name)
		}
		return commandRunner(ctx, password, log, "mysqldump", args, nil)
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

// ResetTables drops the named tables inside c.Database. The rest of the database stays.
func ResetTables(ctx context.Context, c config.Connection, password string, tables []TableRef, log LogFunc) error {
	if len(tables) == 0 {
		return fmt.Errorf("no table selected")
	}
	if !c.Valid() {
		return fmt.Errorf("incomplete connection")
	}
	var b strings.Builder
	labels := make([]string, 0, len(tables))
	switch c.Engine {
	case config.EnginePostgres:
		if _, err := lookPath("psql"); err != nil {
			return fmt.Errorf("missing client tools: psql")
		}
		for _, t := range tables {
			qual, err := qualifiedTable(c, t)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s CASCADE;\n", qual)
			labels = append(labels, t.Label())
		}
		log(fmt.Sprintf("clearing %d tables in %s", len(labels), c.Database))
		log(strings.Join(labels, ", "))
		return commandRunner(ctx, password, log, "psql", psqlArgs(c, c.Database), strings.NewReader(b.String()))
	case config.EngineMySQL:
		if _, err := lookPath("mysql"); err != nil {
			return fmt.Errorf("missing client tools: mysql")
		}
		b.WriteString("SET FOREIGN_KEY_CHECKS=0;\n")
		for _, t := range tables {
			if err := config.ValidateTable(t.Name); err != nil {
				return err
			}
			fmt.Fprintf(&b, "DROP TABLE IF EXISTS %s;\n", quoteMySQL(t.Name))
			labels = append(labels, t.Name)
		}
		log(fmt.Sprintf("clearing %d tables in %s", len(labels), c.Database))
		log(strings.Join(labels, ", "))
		return commandRunner(ctx, password, log, "mysql", mysqlArgs(c, c.Database), strings.NewReader(b.String()))
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

func qualifiedTable(c config.Connection, t TableRef) (string, error) {
	if err := config.ValidateTable(t.Name); err != nil {
		return "", err
	}
	if c.Engine == config.EngineMySQL {
		return quoteMySQL(t.Name), nil
	}
	if t.Schema == "" {
		return quoteIdent(t.Name), nil
	}
	if err := config.ValidateSchema(t.Schema); err != nil {
		return "", err
	}
	return quoteIdent(t.Schema) + "." + quoteIdent(t.Name), nil
}

func touchExport(out string) error {
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	return f.Close()
}
