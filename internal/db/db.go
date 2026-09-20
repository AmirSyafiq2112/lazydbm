package db

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

type LogFunc func(string)

// Test hooks. Production uses exec.LookPath and run; tests replace these.
var (
	lookPath      = exec.LookPath
	commandRunner = run
)

func IsCustomDump(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".dump", ".backup", ".pgdump":
		return true
	default:
		return false
	}
}

func requiredBins(c config.Connection, importFile string, clear, exporting bool) []string {
	need := map[string]struct{}{}
	switch c.Engine {
	case config.EnginePostgres:
		if exporting {
			need["pg_dump"] = struct{}{}
		} else {
			if importFile != "" && IsCustomDump(importFile) {
				need["pg_restore"] = struct{}{}
			}
			need["psql"] = struct{}{}
		}
	case config.EngineMySQL:
		if exporting {
			need["mysqldump"] = struct{}{}
		} else {
			need["mysql"] = struct{}{}
		}
	}
	out := make([]string, 0, len(need))
	for bin := range need {
		out = append(out, bin)
	}
	sort.Strings(out)
	return out
}

func MissingTools(c config.Connection, importFile string, clear, exporting bool) []string {
	var missing []string
	for _, bin := range requiredBins(c, importFile, clear, exporting) {
		if _, err := lookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	return missing
}

func clientBin(c config.Connection) string {
	if c.Engine == config.EngineMySQL {
		return "mysql"
	}
	return "psql"
}

func requireCreds(c config.Connection) error {
	probe := c
	if probe.Database == "" {
		if c.Engine == config.EngineMySQL {
			probe.Database = "mysql"
		} else {
			probe.Database = "postgres"
		}
	}
	if err := probe.ValidateFields(); err != nil {
		return err
	}
	return nil
}

func collectQuery(ctx context.Context, password string, log LogFunc, name string, args []string) ([]string, error) {
	var out []string
	wrap := func(s string) {
		if log != nil {
			log(s)
		}
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "$ ") {
			return
		}
		out = append(out, s)
	}
	err := commandRunner(ctx, password, wrap, name, args, nil)
	return out, err
}

// ListDatabases returns non-template databases on the server.
// Postgres uses the maintenance database "postgres"; MySQL uses a server-level SHOW DATABASES.
func ListDatabases(ctx context.Context, c config.Connection, password string, log LogFunc) ([]string, error) {
	if err := requireCreds(c); err != nil {
		return nil, err
	}
	bin := clientBin(c)
	if _, err := lookPath(bin); err != nil {
		return nil, fmt.Errorf("missing client tools: %s", bin)
	}
	var args []string
	switch c.Engine {
	case config.EnginePostgres:
		args = append(psqlArgs(c, "postgres"), "-X", "-w", "-tA", "-c", "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY 1;")
	case config.EngineMySQL:
		args = append(mysqlArgs(c, ""), "-N", "-e", "SHOW DATABASES")
	default:
		return nil, fmt.Errorf("unsupported engine %s", c.Engine)
	}
	lines, err := collectQuery(ctx, password, log, bin, args)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		if _, ok := seen[line]; ok {
			continue
		}
		if err := config.ValidateDatabase(line); err != nil {
			continue
		}
		seen[line] = struct{}{}
		names = append(names, line)
	}
	sort.Strings(names)
	return names, nil
}

// TestConnection runs SELECT 1 against the connection's database, or the
// engine maintenance database when Database is empty (used while listing).
func TestConnection(ctx context.Context, c config.Connection, password string, log LogFunc) error {
	if err := requireCreds(c); err != nil {
		return err
	}
	bin := clientBin(c)
	if _, err := lookPath(bin); err != nil {
		return fmt.Errorf("missing client tools: %s", bin)
	}
	switch c.Engine {
	case config.EnginePostgres:
		dbName := c.Database
		if dbName == "" {
			dbName = "postgres"
		}
		_, err := collectQuery(ctx, password, log, bin, append(psqlArgs(c, dbName), "-X", "-w", "-c", "SELECT 1"))
		return err
	case config.EngineMySQL:
		dbName := c.Database
		if dbName == "" {
			dbName = "mysql"
		}
		_, err := collectQuery(ctx, password, log, bin, append(mysqlArgs(c, dbName), "-e", "SELECT 1"))
		return err
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

func Import(ctx context.Context, c config.Connection, password, file string, clear bool, log LogFunc) error {
	if file == "" {
		return fmt.Errorf("no dump file selected")
	}
	if !c.Valid() {
		return fmt.Errorf("incomplete connection")
	}
	if IsCustomDump(file) && c.Engine != config.EnginePostgres {
		return fmt.Errorf("custom dump %s is only supported for postgres", filepath.Base(file))
	}
	if missing := MissingTools(c, file, clear, false); len(missing) > 0 {
		return fmt.Errorf("missing client tools: %s", strings.Join(missing, ", "))
	}
	if clear {
		log("clearing database (drop + create)")
		if err := reset(ctx, c, password, log); err != nil {
			return err
		}
	} else if err := EnsureDatabase(ctx, c, password, log); err != nil {
		return err
	}
	switch c.Engine {
	case config.EnginePostgres:
		if IsCustomDump(file) {
			log("importing " + file)
			file = safeCLIFileArg(file)
			return commandRunner(ctx, password, log, "pg_restore", append(pgConnArgs(c, c.Database), "--no-owner", "--no-acl", "--verbose", "--", file), nil)
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		log("importing " + file + " (owners/grants skipped)")
		return commandRunner(ctx, password, log, "psql", psqlArgs(c, c.Database), filterPostgresSQL(f))
	case config.EngineMySQL:
		log("importing " + file)
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		return commandRunner(ctx, password, log, "mysql", mysqlArgs(c, c.Database), f)
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

func Export(ctx context.Context, c config.Connection, password, out string, log LogFunc) error {
	if out == "" {
		return fmt.Errorf("no export path")
	}
	if !c.Valid() {
		return fmt.Errorf("incomplete connection")
	}
	if missing := MissingTools(c, "", false, true); len(missing) > 0 {
		return fmt.Errorf("missing client tools: %s", strings.Join(missing, ", "))
	}
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	log("exporting " + c.Database + " -> " + out)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()

	switch c.Engine {
	case config.EnginePostgres:
		return commandRunner(ctx, password, log, "pg_dump", append(pgConnArgs(c, c.Database), "--no-owner", "--no-acl"), f)
	case config.EngineMySQL:
		return commandRunner(ctx, password, log, "mysqldump", append(mysqlArgs(c, c.Database), "--single-transaction", "--routines", "--triggers"), f)
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

func postgresResetSQL(c config.Connection) string {
	return strings.Join([]string{
		fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid();", quoteLiteral(c.Database)),
		fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quoteIdent(c.Database)),
		fmt.Sprintf("CREATE DATABASE %s OWNER %s;", quoteIdent(c.Database), quoteIdent(c.User)),
	}, "\n")
}

func mysqlResetSQL(c config.Connection) string {
	return fmt.Sprintf("DROP DATABASE IF EXISTS %s; CREATE DATABASE %s;", quoteMySQL(c.Database), quoteMySQL(c.Database))
}

func reset(ctx context.Context, c config.Connection, password string, log LogFunc) error {
	switch c.Engine {
	case config.EnginePostgres:
		log("reset via maintenance database postgres")
		return commandRunner(ctx, password, log, "psql", psqlArgs(c, "postgres"), strings.NewReader(postgresResetSQL(c)))
	case config.EngineMySQL:
		log("reset via server connection")
		return commandRunner(ctx, password, log, "mysql", mysqlArgs(c, ""), strings.NewReader(mysqlResetSQL(c)))
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

func postgresCreateSQL(c config.Connection) string {
	return fmt.Sprintf("CREATE DATABASE %s OWNER %s;", quoteIdent(c.Database), quoteIdent(c.User))
}

func mysqlCreateSQL(c config.Connection) string {
	return fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s;", quoteMySQL(c.Database))
}

func databaseExists(ctx context.Context, c config.Connection, password string, log LogFunc) (bool, error) {
	var (
		name string
		args []string
	)
	switch c.Engine {
	case config.EnginePostgres:
		name = "psql"
		args = append(psqlArgs(c, "postgres"), "-X", "-w", "-tA", "-c",
			fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = %s;", quoteLiteral(c.Database)))
	case config.EngineMySQL:
		name = "mysql"
		args = append(mysqlArgs(c, ""), "-N", "-e",
			fmt.Sprintf("SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = %s;", quoteLiteral(c.Database)))
	default:
		return false, fmt.Errorf("unsupported engine %s", c.Engine)
	}
	rows, err := collectQuery(ctx, password, log, name, args)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// CreateDatabase creates c.Database on the server. MySQL uses IF NOT EXISTS; Postgres errors if it already exists.
func CreateDatabase(ctx context.Context, c config.Connection, password string, log LogFunc) error {
	if err := requireCreds(c); err != nil {
		return err
	}
	if c.Database == "" {
		return fmt.Errorf("database name required")
	}
	bin := clientBin(c)
	if _, err := lookPath(bin); err != nil {
		return fmt.Errorf("missing client tools: %s", bin)
	}
	log("creating database " + c.Database)
	switch c.Engine {
	case config.EnginePostgres:
		return commandRunner(ctx, password, log, "psql", psqlArgs(c, "postgres"), strings.NewReader(postgresCreateSQL(c)))
	case config.EngineMySQL:
		return commandRunner(ctx, password, log, "mysql", mysqlArgs(c, ""), strings.NewReader(mysqlCreateSQL(c)))
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

// EnsureDatabase creates c.Database if it is missing. Existing databases are left unchanged.
func EnsureDatabase(ctx context.Context, c config.Connection, password string, log LogFunc) error {
	if err := requireCreds(c); err != nil {
		return err
	}
	if c.Database == "" {
		return fmt.Errorf("database name required")
	}
	bin := clientBin(c)
	if _, err := lookPath(bin); err != nil {
		return fmt.Errorf("missing client tools: %s", bin)
	}
	exists, err := databaseExists(ctx, c, password, log)
	if err != nil {
		return err
	}
	if exists {
		log("database " + c.Database + " already exists")
		return nil
	}
	return CreateDatabase(ctx, c, password, log)
}

func psqlArgs(c config.Connection, database string) []string {
	return []string{
		"-h", c.Host,
		"-p", strconv.Itoa(c.Port),
		"-U", c.User,
		"-d", database,
		"-v", "ON_ERROR_STOP=1",
	}
}

func pgConnArgs(c config.Connection, database string) []string {
	return []string{
		"-h", c.Host,
		"-p", strconv.Itoa(c.Port),
		"-U", c.User,
		"-d", database,
	}
}

func mysqlArgs(c config.Connection, database string) []string {
	args := []string{
		"-h", c.Host,
		"-P", strconv.Itoa(c.Port),
		"-u", c.User,
		"--connect-timeout=10",
	}
	if database != "" {
		args = append(args, "--", database)
	}
	return args
}

func safeCLIFileArg(path string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "-") {
		return "./" + path
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, "-") {
		dir := filepath.Dir(path)
		if dir == "." || dir == "" {
			return "./" + base
		}
		return filepath.Join(dir, base)
	}
	return path
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func quoteMySQL(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func run(ctx context.Context, password string, log LogFunc, name string, args []string, stdin io.Reader) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = withPassword(os.Environ(), password, name)
	if stdin != nil {
		cmd.Stdin = stdin
	}

	log("$ " + name + " " + strings.Join(redactArgs(args), " "))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	errCh := make(chan error, 2)
	go func() { errCh <- stream(stdout, password, log) }()
	go func() { errCh <- stream(stderr, password, log) }()
	for i := 0; i < 2; i++ {
		if e := <-errCh; e != nil && err == nil {
			err = e
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		return fmt.Errorf("%s: %w", name, waitErr)
	}
	return err
}

func stream(r io.Reader, password string, log LogFunc) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		log(redact(sc.Text(), password))
	}
	return sc.Err()
}

func withPassword(env []string, password, bin string) []string {
	key := "PGPASSWORD"
	if strings.Contains(bin, "mysql") {
		key = "MYSQL_PWD"
	}
	out := make([]string, 0, len(env)+1)
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	out = append(out, prefix+password)
	return out
}

func redact(s, password string) string {
	if password != "" {
		s = strings.ReplaceAll(s, password, "********")
	}
	return s
}

func redactArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	return out
}
