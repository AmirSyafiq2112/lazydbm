package discover

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
	"gopkg.in/yaml.v3"
)

type publishedPort struct {
	Port int
}

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string `yaml:"image"`
	Environment any    `yaml:"environment"`
	Ports       []any  `yaml:"ports"`
}

func fromCompose(cwd string) ([]config.Connection, []string, map[string]publishedPort, error) {
	names := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	hosts := map[string]publishedPort{}
	var conns []config.Connection
	var passwords []string

	for _, name := range names {
		path := filepath.Join(cwd, name)
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, nil, err
		}
		var cf composeFile
		if err := yaml.Unmarshal(b, &cf); err != nil {
			continue
		}
		for svcName, svc := range cf.Services {
			c, pw, ok := serviceConnection(svcName, svc, name)
			if !ok {
				continue
			}
			if pub := publishedHostPort(svc, defaultPort(c.Engine)); pub > 0 {
				hosts[svcName] = publishedPort{Port: pub}
				c.Port = pub
			}
			c.Host = "127.0.0.1"
			conns = append(conns, c)
			passwords = append(passwords, pw)
		}
	}
	return conns, passwords, hosts, nil
}

func serviceConnection(svcName string, svc composeService, source string) (config.Connection, string, bool) {
	env := parseComposeEnv(svc.Environment)
	engine, ok := engineFromService(svc.Image, svcName, env)
	if !ok {
		return config.Connection{}, "", false
	}

	c := config.Connection{
		Engine: engine,
		Host:   "127.0.0.1",
		Port:   defaultPort(engine),
		Source: source + ":" + svcName,
	}
	pw := ""
	switch engine {
	case config.EnginePostgres:
		c.User = first(env, "POSTGRES_USER", "POSTGRESQL_USER")
		if c.User == "" {
			c.User = "postgres"
		}
		c.Database = first(env, "POSTGRES_DB", "POSTGRESQL_DB", "POSTGRES_DATABASE")
		if c.Database == "" {
			c.Database = c.User
		}
		pw = first(env, "POSTGRES_PASSWORD", "POSTGRESQL_PASSWORD")
	case config.EngineMySQL:
		c.Database = first(env, "MYSQL_DATABASE", "MARIADB_DATABASE")
		c.User = first(env, "MYSQL_USER", "MARIADB_USER")
		pw = first(env, "MYSQL_PASSWORD", "MARIADB_PASSWORD")
		if c.User == "" {
			c.User = "root"
			if rootPW := first(env, "MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD"); rootPW != "" {
				pw = rootPW
			}
		}
	}
	if c.Database != "" {
		c.Name = c.Database + " (" + svcName + ")"
	} else {
		c.Name = svcName
	}
	if !c.ValidServer() {
		return config.Connection{}, "", false
	}
	return c, pw, true
}

func engineFromService(image, svcName string, env map[string]string) (config.Engine, bool) {
	img := strings.ToLower(image)
	base := img
	if i := strings.LastIndex(img, "/"); i >= 0 {
		base = img[i+1:]
	}
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	if e, ok := config.NormalizeEngine(base); ok {
		return e, true
	}
	switch {
	case strings.Contains(img, "postgres"):
		return config.EnginePostgres, true
	case strings.Contains(img, "mysql"), strings.Contains(img, "mariadb"), strings.Contains(img, "percona"):
		return config.EngineMySQL, true
	}
	if first(env, "POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD") != "" {
		return config.EnginePostgres, true
	}
	if first(env, "MYSQL_DATABASE", "MYSQL_USER", "MYSQL_ROOT_PASSWORD") != "" {
		return config.EngineMySQL, true
	}
	if e, ok := config.NormalizeEngine(svcName); ok {
		return e, true
	}
	return "", false
}

func parseComposeEnv(v any) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			out[k] = stringify(val)
		}
	case map[any]any:
		for k, val := range t {
			out[stringify(k)] = stringify(val)
		}
	case []any:
		for _, item := range t {
			s := stringify(item)
			k, val, ok := strings.Cut(s, "=")
			if ok {
				out[k] = val
			}
		}
	}
	return out
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.Itoa(int(t))
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func publishedHostPort(svc composeService, targetDefault int) int {
	for _, p := range svc.Ports {
		host, target := parsePortMapping(p)
		if host == 0 {
			continue
		}
		if target == 0 || target == targetDefault {
			return host
		}
	}
	for _, p := range svc.Ports {
		host, _ := parsePortMapping(p)
		if host > 0 {
			return host
		}
	}
	return 0
}

func parsePortMapping(v any) (host, target int) {
	switch t := v.(type) {
	case string:
		s := t
		if i := strings.IndexByte(s, '/'); i >= 0 {
			s = s[:i]
		}
		parts := strings.Split(s, ":")
		switch len(parts) {
		case 1:
			n, _ := strconv.Atoi(parts[0])
			return n, n
		case 2:
			h, _ := strconv.Atoi(parts[0])
			tg, _ := strconv.Atoi(parts[1])
			return h, tg
		default:
			h, _ := strconv.Atoi(parts[len(parts)-2])
			tg, _ := strconv.Atoi(parts[len(parts)-1])
			return h, tg
		}
	case int:
		return t, t
	case int64:
		return int(t), int(t)
	case map[string]any:
		host = anyInt(t["published"])
		target = anyInt(t["target"])
		return host, target
	case map[any]any:
		m := map[string]any{}
		for k, val := range t {
			m[stringify(k)] = val
		}
		return parsePortMapping(m)
	}
	return 0, 0
}

func anyInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}
