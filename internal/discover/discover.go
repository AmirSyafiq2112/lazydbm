package discover

import (
	"sort"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

type Result struct {
	Connections  []config.Connection
	EnvPasswords map[string]string
	Files        []string
}

func Scan(cwd string, saved []config.Connection) (Result, error) {
	res := Result{EnvPasswords: map[string]string{}}
	seen := map[string]int{}

	add := func(c config.Connection, password string) {
		if !c.Valid() {
			return
		}
		id := c.ID()
		if idx, ok := seen[id]; ok {
			if password != "" {
				res.EnvPasswords[id] = password
			}
			if c.Name != "" && res.Connections[idx].Name == "" {
				res.Connections[idx].Name = c.Name
			}
			return
		}
		seen[id] = len(res.Connections)
		res.Connections = append(res.Connections, c)
		if password != "" {
			res.EnvPasswords[id] = password
		}
	}

	envConns, envPWs, err := fromEnvFiles(cwd)
	if err != nil {
		return Result{}, err
	}
	composeConns, composePWs, composeHosts, err := fromCompose(cwd)
	if err != nil {
		return Result{}, err
	}

	for i, c := range envConns {
		if pub, ok := composeHosts[c.Host]; ok {
			c.Host = "127.0.0.1"
			if pub.Port > 0 {
				c.Port = pub.Port
			}
			if c.Source == "" {
				c.Source = ".env+compose"
			}
		}
		add(c, envPWs[i])
	}
	for i, c := range composeConns {
		add(c, composePWs[i])
	}
	for _, c := range saved {
		add(c, "")
	}

	sort.SliceStable(res.Connections, func(i, j int) bool {
		a, b := res.Connections[i], res.Connections[j]
		if a.Database != b.Database {
			return a.Database < b.Database
		}
		return a.ID() < b.ID()
	})
	seen = map[string]int{}
	for i, c := range res.Connections {
		seen[c.ID()] = i
	}

	files, err := dumpFiles(cwd)
	if err != nil {
		return Result{}, err
	}
	res.Files = files
	return res, nil
}
