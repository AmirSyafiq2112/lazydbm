package discover

import (
	"bufio"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

func fromEnvFiles(cwd string) ([]config.Connection, []string, []bool, error) {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return nil, nil, nil, err
	}

	var conns []config.Connection
	var passwords []string
	var hasPW []bool
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isEnvFile(name) {
			continue
		}
		c, pw, has, ok, err := parseEnvConnection(filepath.Join(cwd, name), name)
		if err != nil {
			continue
		}
		if !ok {
			continue
		}
		conns = append(conns, c)
		passwords = append(passwords, pw)
		hasPW = append(hasPW, has)
	}
	return conns, passwords, hasPW, nil
}

func isEnvFile(name string) bool {
	if name == ".env" {
		return true
	}
	if !strings.HasPrefix(name, ".env.") {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".example") || strings.HasSuffix(lower, ".sample") {
		return false
	}
	return true
}

func parseEnvConnection(path, source string) (config.Connection, string, bool, bool, error) {
	kv, err := parseEnvFile(path)
	if err != nil {
		return config.Connection{}, "", false, false, err
	}

	if raw := first(kv, "DATABASE_URL", "DB_URL"); raw != "" {
		c, pw, hasPW, ok := parseDatabaseURL(raw, source)
		if ok {
			return c, pw, hasPW, true, nil
		}
	}

	engine, ok := config.NormalizeEngine(first(kv, "DB_CONNECTION", "DB_DRIVER"))
	if !ok {
		return config.Connection{}, "", false, false, nil
	}

	c := config.Connection{
		Engine:   engine,
		Host:     first(kv, "DB_HOST", "DB_HOSTNAME"),
		User:     first(kv, "DB_USERNAME", "DB_USER"),
		Database: first(kv, "DB_DATABASE", "DB_NAME"),
		Source:   source,
		Port:     defaultPort(engine),
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if p := first(kv, "DB_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			c.Port = n
		}
	}
	if c.Name == "" {
		if c.Database != "" {
			c.Name = c.Database + " (" + source + ")"
		} else {
			c.Name = source
		}
	}
	if !c.ValidServer() {
		return config.Connection{}, "", false, false, nil
	}
	pw, hasPW := lookup(kv, "DB_PASSWORD", "DB_PASS")
	return c, pw, hasPW, true, nil
}

func parseDatabaseURL(raw, source string) (config.Connection, string, bool, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return config.Connection{}, "", false, false
	}
	engine, ok := config.NormalizeEngine(u.Scheme)
	if !ok {
		return config.Connection{}, "", false, false
	}
	port := defaultPort(engine)
	if u.Port() != "" {
		if n, err := strconv.Atoi(u.Port()); err == nil {
			port = n
		}
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if i := strings.IndexByte(dbName, '/'); i >= 0 {
		dbName = dbName[:i]
	}
	user := ""
	pw := ""
	hasPW := false
	if u.User != nil {
		user = u.User.Username()
		pw, hasPW = u.User.Password()
	}
	host := u.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	c := config.Connection{
		Name:     dbName + " (" + source + ")",
		Engine:   engine,
		Host:     host,
		Port:     port,
		User:     user,
		Database: dbName,
		Source:   source,
	}
	if !c.Valid() {
		return config.Connection{}, "", false, false
	}
	return c, pw, hasPW, true
}

func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = unquote(val)
		if key != "" {
			out[key] = val
		}
	}
	return out, sc.Err()
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	if i := strings.Index(s, " #"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func first(kv map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := kv[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func lookup(kv map[string]string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := kv[k]; ok {
			return v, true
		}
	}
	return "", false
}

func defaultPort(e config.Engine) int {
	if e == config.EngineMySQL {
		return 3306
	}
	return 5432
}
