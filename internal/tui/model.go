package tui

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
	"github.com/AmirSyafiq2112/lazydbm/internal/discover"
	"github.com/AmirSyafiq2112/lazydbm/internal/job"
	"github.com/AmirSyafiq2112/lazydbm/internal/secret"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pane int

const (
	paneConns pane = iota
	paneFiles
	paneLogs
)

type overlay int

const (
	overlayNone overlay = iota
	overlayPassword
	overlaySavePassword
	overlayConfirmImport
	overlayExportPath
	overlayHelp
	overlayNotice
)

type model struct {
	cwd     string
	version string
	width   int
	height  int
	ready   bool

	cfg    config.File
	conns  []config.Connection
	files  []string
	envPW  map[string]string
	memPW  map[string]string
	hasKey map[string]bool

	connIdx int
	fileIdx int
	focus   pane
	clearDB bool

	runner   *job.Runner
	logVP    viewport.Model
	stickLog bool

	overlay   overlay
	notice    string
	pwInput   textinput.Model
	pathInput textinput.Model

	store   secret.Store
	saveCfg func(config.File) error
}

func Run(cwd, version string) error {
	m := newModel(cwd, version)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(cwd, version string) model {
	pw := themeInput(textinput.New())
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '•'
	pw.Placeholder = "password"
	pw.Prompt = "> "
	pw.CharLimit = 512

	path := themeInput(textinput.New())
	path.Placeholder = "export path"
	path.Prompt = "> "
	path.CharLimit = 1024

	return model{
		cwd:       cwd,
		version:   version,
		envPW:     map[string]string{},
		memPW:     map[string]string{},
		hasKey:    map[string]bool{},
		clearDB:   false,
		runner:    job.New(),
		stickLog:  true,
		pwInput:   pw,
		pathInput: path,
		logVP:     viewport.New(20, 10),
		store:     secret.Default,
		saveCfg:   config.Save,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(reload(m.cwd), tick())
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type discoveredMsg struct {
	cfg  config.File
	res  discover.Result
	keys map[string]bool
	err  error
}

func reload(cwd string) tea.Cmd {
	return func() tea.Msg {
		return loadDiscovered(cwd, secret.Default)
	}
}

func loadDiscovered(cwd string, st secret.Store) discoveredMsg {
	if st == nil {
		st = secret.Default
	}
	cfg, err := config.Load()
	if err != nil {
		return discoveredMsg{err: fmt.Errorf("config: %w", err)}
	}
	res, err := discover.Scan(cwd, cfg.Connections)
	if err != nil {
		return discoveredMsg{cfg: cfg, err: fmt.Errorf("discover: %w", err)}
	}
	keys := map[string]bool{}
	for _, c := range res.Connections {
		if pw, err := st.Get(c.ID()); err == nil && pw != "" {
			keys[c.ID()] = true
		}
	}
	return discoveredMsg{cfg: cfg, res: res, keys: keys}
}

func (m model) currentConn() (config.Connection, bool) {
	if m.connIdx < 0 || m.connIdx >= len(m.conns) {
		return config.Connection{}, false
	}
	return m.conns[m.connIdx], true
}

func (m model) currentFile() (string, bool) {
	if m.fileIdx < 0 || m.fileIdx >= len(m.files) {
		return "", false
	}
	return m.files[m.fileIdx], true
}

func (m model) passwordFor(c config.Connection) (string, string) {
	id := c.ID()
	if pw := m.memPW[id]; pw != "" {
		return pw, "mem"
	}
	if pw := m.envPW[id]; pw != "" {
		return pw, "env"
	}
	if m.hasKey[id] {
		st := m.store
		if st == nil {
			st = secret.Default
		}
		if pw, err := st.Get(id); err == nil && pw != "" {
			return pw, "key"
		}
	}
	return "", ""
}

func (m model) persistLastUsed(c config.Connection) model {
	m.cfg.LastUsed = c.ID()
	m.cfg.Upsert(c)
	save := m.saveCfg
	if save == nil {
		save = config.Save
	}
	_ = save(m.cfg)
	return m
}

var now = time.Now

func defaultExportPath(c config.Connection) string {
	return fmt.Sprintf("%s-%s.sql", c.Database, now().Format("20060102-150405"))
}

func exportAbs(cwd, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}

func themeInput(ti textinput.Model) textinput.Model {
	ti.PromptStyle = lipgloss.NewStyle()
	ti.TextStyle = lipgloss.NewStyle()
	ti.PlaceholderStyle = lipgloss.NewStyle().Faint(true)
	ti.CompletionStyle = lipgloss.NewStyle().Faint(true)
	return ti
}
