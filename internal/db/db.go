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

func IsCustomDump(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".dump", ".backup", ".pgdump":
		return true
	default:
		return false
	}
}

func MissingTools(c config.Connection, importFile string, clear, exporting bool) []string {
	need := map[string]struct{}{}
	switch c.Engine {
	case config.EnginePostgres:
		if exporting {
			need["pg_dump"] = struct{}{}
		} else {
			if importFile != "" && IsCustomDump(importFile) {
				need["pg_restore"] = struct{}{}
			} else {
				need["psql"] = struct{}{}
			}
			if clear {
				need["psql"] = struct{}{}
			}
		}
	case config.EngineMySQL:
		if exporting {
			need["mysqldump"] = struct{}{}
		} else {
			need["mysql"] = struct{}{}
		}
	}
	var missing []string
	for bin := range need {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	sort.Strings(missing)
	return missing
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
	}
	log("importing " + file)
	switch c.Engine {
	case config.EnginePostgres:
		if IsCustomDump(file) {
			return run(ctx, password, log, "pg_restore", append(pgConnArgs(c, c.Database), "--no-owner", "--no-acl", "--verbose", file), nil)
		}
		return run(ctx, password, log, "psql", append(psqlArgs(c, c.Database), "-f", file), nil)
	case config.EngineMySQL:
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		return run(ctx, password, log, "mysql", mysqlArgs(c, c.Database), f)
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
		return run(ctx, password, log, "pg_dump", append(pgConnArgs(c, c.Database), "--no-owner", "--no-acl"), f)
	case config.EngineMySQL:
		return run(ctx, password, log, "mysqldump", append(mysqlArgs(c, c.Database), "--single-transaction", "--routines", "--triggers"), f)
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
}

func reset(ctx context.Context, c config.Connection, password string, log LogFunc) error {
	switch c.Engine {
	case config.EnginePostgres:
		sql := strings.Join([]string{
			fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid();", quoteLiteral(c.Database)),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quoteIdent(c.Database)),
			fmt.Sprintf("CREATE DATABASE %s OWNER %s;", quoteIdent(c.Database), quoteIdent(c.User)),
		}, "\n")
		log("reset via maintenance database postgres")
		return run(ctx, password, log, "psql", psqlArgs(c, "postgres"), strings.NewReader(sql))
	case config.EngineMySQL:
		sql := fmt.Sprintf("DROP DATABASE IF EXISTS %s; CREATE DATABASE %s;", quoteMySQL(c.Database), quoteMySQL(c.Database))
		log("reset via server connection")
		return run(ctx, password, log, "mysql", mysqlArgs(c, ""), strings.NewReader(sql))
	default:
		return fmt.Errorf("unsupported engine %s", c.Engine)
	}
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
		args = append(args, database)
	}
	return args
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
