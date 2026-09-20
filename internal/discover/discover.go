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

	add := func(c config.Connection, password string, recordPW bool) {
		if !c.ValidServer() {
			return
		}
		id := c.ID()
		if idx, ok := seen[id]; ok {
			if recordPW {
				if _, exists := res.EnvPasswords[id]; !exists || password != "" {
					res.EnvPasswords[id] = password
				}
			}
			if c.Name != "" && res.Connections[idx].Name == "" {
				res.Connections[idx].Name = c.Name
			}
			if res.Connections[idx].Database == "" && c.Database != "" {
				res.Connections[idx].Database = c.Database
			}
			if res.Connections[idx].LastDatabase == "" && c.LastDatabase != "" {
				res.Connections[idx].LastDatabase = c.LastDatabase
			}
			if c.NoPassword {
				res.Connections[idx].NoPassword = true
			}
			return
		}
		seen[id] = len(res.Connections)
		res.Connections = append(res.Connections, c)
		if recordPW {
			res.EnvPasswords[id] = password
		}
	}

	envConns, envPWs, envHasPW, err := fromEnvFiles(cwd)
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
		add(c, envPWs[i], envHasPW[i])
	}
	for i, c := range composeConns {
		add(c, composePWs[i], composePWs[i] != "")
	}
	for _, c := range saved {
		add(c, "", false)
	}

	sort.SliceStable(res.Connections, func(i, j int) bool {
		return res.Connections[i].ID() < res.Connections[j].ID()
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
