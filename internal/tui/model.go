package tui

import (
	"fmt"
	"path/filepath"
	"strings"
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
	paneServers pane = iota
	paneDBs
	paneFiles
	paneLogs
	paneCount
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
	overlayAdd
	overlayEdit
	overlayConfirmDelete
	overlayCreateDB
)

type pendingKind int

const (
	pendingNone pendingKind = iota
	pendingImport
	pendingExport
	pendingConnect
	pendingCreateDB
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

	connIdx   int
	fileIdx   int
	dbIdx     int
	focus     pane
	clearDB   bool
	dbReady   bool
	dbErr     string
	databases []string
	dbLists   map[string][]string
	dbErrors  map[string]string

	runner   *job.Runner
	logVP    viewport.Model
	stickLog bool

	overlay   overlay
	notice    string
	pwInput   textinput.Model
	pathInput textinput.Model
	dbInput   textinput.Model

	formInputs  []textinput.Model
	formFocus   int
	formEngine  config.Engine
	formSaveKey bool
	formEditID  string
	formErr     string
	pending     pendingKind

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
	pw.Placeholder = ""
	pw.Prompt = "> "
	pw.CharLimit = 512

	path := themeInput(textinput.New())
	path.Placeholder = "export path"
	path.Prompt = "> "
	path.CharLimit = 1024

	dbName := themeInput(textinput.New())
	dbName.Placeholder = "new database name"
	dbName.Prompt = "> "
	dbName.CharLimit = 128

	return model{
		cwd:         cwd,
		version:     version,
		envPW:       map[string]string{},
		memPW:       map[string]string{},
		hasKey:      map[string]bool{},
		dbLists:     map[string][]string{},
		dbErrors:    map[string]string{},
		clearDB:     false,
		runner:      job.New(),
		stickLog:    true,
		pwInput:     pw,
		pathInput:   path,
		dbInput:     dbName,
		logVP:       viewport.New(20, 10),
		store:       secret.Default,
		saveCfg:     config.Save,
		formInputs:  newFormInputs(),
		formEngine:  config.EnginePostgres,
		formSaveKey: true,
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

func (m model) currentDB() (string, bool) {
	if !m.dbReady || m.dbErr != "" {
		return "", false
	}
	if m.dbIdx < 0 || m.dbIdx >= len(m.databases) {
		return "", false
	}
	name := m.databases[m.dbIdx]
	if err := config.ValidateDatabase(name); err != nil {
		return "", false
	}
	return name, true
}

func (m model) selectedTarget() (config.Connection, bool) {
	c, ok := m.currentConn()
	if !ok {
		return config.Connection{}, false
	}
	db, ok := m.currentDB()
	if !ok {
		return config.Connection{}, false
	}
	return c.WithDatabase(db), true
}

func (m model) passwordFor(c config.Connection) (string, string) {
	return m.lookupPassword(c.ID(), c.NoPassword || m.connByID(c.ID()).NoPassword)
}

func (m model) lookupPassword(id string, noPassword bool) (string, string) {
	if id == "" {
		return "", ""
	}
	if pw, ok := m.memPW[id]; ok {
		if pw == "" {
			return "", "none"
		}
		return pw, "mem"
	}
	if pw, ok := m.envPW[id]; ok {
		if pw == "" {
			return "", "none"
		}
		return pw, "env"
	}
	if noPassword {
		return "", "none"
	}
	if m.hasKey[id] {
		st := m.store
		if st == nil {
			st = secret.Default
		}
		if pw, err := st.Get(id); err == nil {
			if pw == "" {
				return "", "none"
			}
			return pw, "key"
		}
	}
	return "", ""
}

func (m model) persistLastUsed(c config.Connection) model {
	if db, ok := m.currentDB(); ok {
		c.LastDatabase = db
	}
	m.cfg.LastUsed = c.ID()
	if c.Saved() {
		m.cfg.Upsert(c)
	}
	if i := indexByID(m.conns, c.ID()); i >= 0 {
		m.conns[i].LastDatabase = c.LastDatabase
	}
	save := m.saveCfg
	if save == nil {
		save = config.Save
	}
	_ = save(m.cfg)
	return m
}

func (m *model) showDBsForCurrent() {
	c, ok := m.currentConn()
	if !ok {
		m.databases = nil
		m.dbIdx = 0
		m.dbReady = false
		m.dbErr = ""
		return
	}
	id := c.ID()
	if err, ok := m.dbErrors[id]; ok {
		m.databases = nil
		m.dbIdx = 0
		m.dbReady = false
		m.dbErr = err
		return
	}
	if names, ok := m.dbLists[id]; ok {
		m.databases = names
		m.dbErr = ""
		m.dbReady = true
		m.dbIdx = pickDBIndex(names, c.LastDatabase, c.Database)
		return
	}
	m.databases = nil
	m.dbIdx = 0
	m.dbReady = false
	m.dbErr = ""
}

func (m *model) pruneDBCache() {
	alive := map[string]struct{}{}
	for _, c := range m.conns {
		alive[c.ID()] = struct{}{}
	}
	for id := range m.dbLists {
		if _, ok := alive[id]; !ok {
			delete(m.dbLists, id)
		}
	}
	for id := range m.dbErrors {
		if _, ok := alive[id]; !ok {
			delete(m.dbErrors, id)
		}
	}
}

func pickDBIndex(names []string, preferred ...string) int {
	for _, want := range preferred {
		if want == "" {
			continue
		}
		for i, n := range names {
			if n == want {
				return i
			}
		}
	}
	if len(names) == 0 {
		return 0
	}
	return 0
}

var now = time.Now

func safeExportBasename(name string) (string, error) {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("invalid export name")
	}
	if strings.Contains(base, "..") || strings.ContainsAny(base, `/\`) {
		return "", fmt.Errorf("invalid export name")
	}
	return base, nil
}

func defaultExportPath(dbName string) string {
	raw := fmt.Sprintf("%s-%s.sql", dbName, now().Format("20060102-150405"))
	base, err := safeExportBasename(raw)
	if err != nil {
		return fmt.Sprintf("export-%s.sql", now().Format("20060102-150405"))
	}
	return base
}

func resolveExportPath(cwd, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("no export path")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	root, err := absDir(cwd)
	if err != nil {
		return "", err
	}
	joined := filepath.Clean(filepath.Join(root, path))
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("export path must stay under the current directory")
	}
	return joined, nil
}

func absDir(cwd string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if eval, err := filepath.EvalSymlinks(abs); err == nil {
		return eval, nil
	}
	return abs, nil
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
