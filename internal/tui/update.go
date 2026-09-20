package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
	"github.com/AmirSyafiq2112/lazydbm/internal/db"
	"github.com/AmirSyafiq2112/lazydbm/internal/secret"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	importDB     = db.Import
	exportDB     = db.Export
	listDBs      = db.ListDatabases
	testConn     = db.TestConnection
	missingTools = db.MissingTools
	ensureDB     = db.EnsureDatabase
)

func (m model) storeOrDefault() secret.Store {
	if m.store != nil {
		return m.store
	}
	return secret.Default
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.sizeLogPane()
		return m, nil

	case discoveredMsg:
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.overlay = overlayNotice
			return m, nil
		}
		m.cfg = msg.cfg
		m.conns = msg.res.Connections
		m.files = msg.res.Files
		m.envPW = msg.res.EnvPasswords
		if m.envPW == nil {
			m.envPW = map[string]string{}
		}
		m.hasKey = msg.keys
		if m.hasKey == nil {
			m.hasKey = map[string]bool{}
		}
		m.connIdx = indexByID(m.conns, m.cfg.LastUsed)
		if m.connIdx < 0 && len(m.conns) > 0 {
			m.connIdx = 0
		}
		if m.fileIdx >= len(m.files) {
			m.fileIdx = 0
		}
		m.pruneDBCache()
		m.showDBsForCurrent()
		return m, nil

	case tickMsg:
		m.syncLogs()
		return m, tick()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.runner.Running() {
			m.runner.Cancel()
			return m, nil
		}
		return m, tea.Quit
	case "q":
		if m.runner.Running() {
			m.runner.Cancel()
		}
		return m, tea.Quit
	case "tab":
		m.focus = (m.focus + 1) % paneCount
		return m, nil
	case "shift+tab":
		m.focus = (m.focus + paneCount - 1) % paneCount
		return m, nil
	case "h", "left":
		if m.focus == paneLogs {
			m.focus = paneFiles
			return m, nil
		}
		if m.focus > paneServers {
			m.focus--
		}
		return m, nil
	case "l", "right":
		if m.focus == paneLogs {
			return m, nil
		}
		if m.focus < paneFiles {
			m.focus++
		}
		return m, nil
	case "?":
		m.overlay = overlayHelp
		return m, nil
	case "r":
		return m.refreshAll()
	case "c":
		m.clearDB = !m.clearDB
		return m, nil
	case "p":
		return m.beginPassword()
	case "n":
		return m.beginCreateDB()
	case "i":
		return m.beginImport()
	case "e":
		return m.beginExport()
	case "a":
		return m.beginAdd()
	case "E":
		if m.focus == paneServers {
			return m.beginEdit()
		}
	case "d":
		if m.focus == paneServers {
			return m.beginDelete()
		}
	case "enter":
		if m.focus == paneFiles {
			return m.beginImport()
		}
		if m.focus == paneServers {
			return m.connectServer()
		}
		return m, nil
	}

	switch m.focus {
	case paneServers:
		prev := m.connIdx
		m.connIdx = moveIndex(m.connIdx, len(m.conns), msg.String())
		if m.connIdx != prev {
			m.showDBsForCurrent()
		}
	case paneDBs:
		if m.dbReady && m.dbErr == "" {
			m.dbIdx = moveIndex(m.dbIdx, len(m.databases), msg.String())
		}
	case paneFiles:
		m.fileIdx = moveIndex(m.fileIdx, len(m.files), msg.String())
	case paneLogs:
		m.handleLogKeys(msg.String())
	}
	return m, nil
}

func (m model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayHelp, overlayNotice:
		if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" || msg.String() == "?" {
			m.overlay = overlayNone
			m.notice = ""
		}
		return m, nil

	case overlaySavePassword:
		switch msg.String() {
		case "y", "Y":
			return m.savePassword(true)
		case "n", "N", "esc":
			return m.savePassword(false)
		}
		return m, nil

	case overlayCreateDB:
		switch msg.String() {
		case "esc":
			m.overlay = overlayNone
			m.dbInput.Blur()
			return m, nil
		case "enter":
			m.overlay = overlayNone
			m.dbInput.Blur()
			return m.finishCreateDB(strings.TrimSpace(m.dbInput.Value()))
		}
		var cmd tea.Cmd
		m.dbInput, cmd = m.dbInput.Update(msg)
		return m, cmd

	case overlayConfirmImport:
		switch msg.String() {
		case "esc":
			m.overlay = overlayNone
			return m, nil
		case "c":
			m.clearDB = !m.clearDB
			return m, nil
		case "enter", "y", "Y":
			m.overlay = overlayNone
			return m.startImportJob()
		}
		return m, nil

	case overlayPassword:
		switch msg.String() {
		case "esc":
			m.overlay = overlayNone
			m.pwInput.Blur()
			return m, nil
		case "enter":
			m.overlay = overlaySavePassword
			m.pwInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.pwInput, cmd = m.pwInput.Update(msg)
		return m, cmd

	case overlayExportPath:
		switch msg.String() {
		case "esc":
			m.overlay = overlayNone
			m.pathInput.Blur()
			return m, nil
		case "enter":
			m.overlay = overlayNone
			m.pathInput.Blur()
			return m.startExportJob(strings.TrimSpace(m.pathInput.Value()))
		}
		var cmd tea.Cmd
		m.pathInput, cmd = m.pathInput.Update(msg)
		return m, cmd

	case overlayAdd, overlayEdit:
		return m.handleFormKey(msg)

	case overlayConfirmDelete:
		switch msg.String() {
		case "esc":
			m.overlay = overlayNone
			return m, nil
		case "enter", "y", "Y":
			return m.confirmDelete()
		}
		return m, nil
	}
	return m, nil
}

func (m model) beginPassword() (tea.Model, tea.Cmd) {
	if _, ok := m.currentConn(); !ok {
		return m.noticeAndLog("select a server first")
	}
	m.overlay = overlayPassword
	m.pwInput.SetValue("")
	m.pwInput.Placeholder = "leave empty for trust"
	m.pwInput.Focus()
	return m, textinput.Blink
}

func (m model) savePassword(persist bool) (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	m.overlay = overlayNone
	if !ok {
		m.pending = pendingNone
		return m, nil
	}
	pw := m.pwInput.Value()
	m.memPW[c.ID()] = pw
	none := pw == ""
	keyErr := ""
	if persist {
		c.NoPassword = none
		if i := indexByID(m.conns, c.ID()); i >= 0 {
			m.conns[i].NoPassword = none
		}
		if none {
			_ = m.storeOrDefault().Delete(c.ID())
			m.hasKey[c.ID()] = false
			m = m.persistLastUsed(c)
		} else if err := m.storeOrDefault().Set(c.ID(), pw); err != nil {
			m.hasKey[c.ID()] = false
			keyErr = "keychain save failed: " + err.Error() + " (using this session only)"
			m = m.appendLog(keyErr)
		} else {
			m.hasKey[c.ID()] = true
			m = m.persistLastUsed(c)
		}
	}
	if keyErr != "" && m.pending == pendingNone {
		return m.showNotice(keyErr)
	}
	return m.afterPassword()
}

func (m model) afterPassword() (tea.Model, tea.Cmd) {
	switch m.pending {
	case pendingImport:
		m.pending = pendingNone
		m.overlay = overlayConfirmImport
		return m, nil
	case pendingExport:
		m.pending = pendingNone
		return m.beginExport()
	case pendingConnect:
		m.pending = pendingNone
		return m.connectServer()
	case pendingCreateDB:
		m.pending = pendingNone
		return m.beginCreateDB()
	default:
		return m, nil
	}
}

func (m model) connectServer() (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m.noticeAndLog("select a server first")
	}
	if _, src := m.passwordFor(c); src == "" {
		m.pending = pendingConnect
		return m.beginPassword()
	}
	m = m.persistLastUsed(c)
	return m.listServerDatabases(c)
}

func (m model) refreshAll() (tea.Model, tea.Cmd) {
	cmd := reload(m.cwd)
	c, ok := m.currentConn()
	if !ok {
		return m, cmd
	}
	if _, src := m.passwordFor(c); src == "" {
		return m, cmd
	}
	tm, _ := m.listServerDatabases(c)
	return tm, cmd
}

func (m model) listServerDatabases(c config.Connection) (tea.Model, tea.Cmd) {
	pw, _ := m.passwordFor(c)
	id := c.ID()
	m.stickLog = true
	m = m.appendLog("connect " + c.Short())
	if err := testConn(context.Background(), c, pw, m.runner.Append); err != nil {
		m.databases = nil
		m.dbIdx = 0
		m.dbReady = false
		m.dbErr = err.Error()
		delete(m.dbLists, id)
		m.dbErrors[id] = m.dbErr
		m.syncLogs()
		return m.noticeAndLog("connect failed: " + err.Error())
	}
	names, err := listDBs(context.Background(), c, pw, m.runner.Append)
	m.syncLogs()
	if err != nil {
		m.databases = nil
		m.dbIdx = 0
		m.dbReady = false
		m.dbErr = err.Error()
		delete(m.dbLists, id)
		m.dbErrors[id] = m.dbErr
		return m.noticeAndLog("list databases: " + err.Error())
	}
	m.dbErr = ""
	m.databases = names
	m.dbReady = true
	m.dbIdx = pickDBIndex(names, c.LastDatabase, c.Database)
	m.dbLists[id] = names
	delete(m.dbErrors, id)
	m = m.appendLog(fmt.Sprintf("listed %d databases", len(names)))
	m = m.persistLastUsed(c)
	return m, nil
}

func (m model) beginCreateDB() (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m.noticeAndLog("select a server first")
	}
	if _, src := m.passwordFor(c); src == "" {
		m.pending = pendingCreateDB
		return m.beginPassword()
	}
	m.pending = pendingNone
	m.overlay = overlayCreateDB
	m.dbInput.SetValue("")
	m.dbInput.Placeholder = "new database name"
	m.dbInput.Focus()
	return m, textinput.Blink
}

func (m model) finishCreateDB(name string) (tea.Model, tea.Cmd) {
	name = strings.TrimSpace(name)
	if err := config.ValidateDatabase(name); err != nil {
		return m.noticeAndLog(err.Error())
	}
	c, ok := m.currentConn()
	if !ok {
		return m.noticeAndLog("select a server first")
	}
	if _, src := m.passwordFor(c); src == "" {
		m.dbInput.SetValue(name)
		m.pending = pendingCreateDB
		return m.beginPassword()
	}
	for i, n := range m.databases {
		if n == name {
			m.dbIdx = i
			m.focus = paneDBs
			m = m.appendLog("using existing database " + name)
			return m, nil
		}
	}
	if m.runner.Running() {
		return m.noticeAndLog("a job is already running")
	}
	pw, _ := m.passwordFor(c)
	target := c.WithDatabase(name)
	m.stickLog = true
	m = m.appendLog("creating database " + name)
	if err := ensureDB(context.Background(), target, pw, m.runner.Append); err != nil {
		m.syncLogs()
		return m.noticeAndLog("create database: " + err.Error())
	}
	m.syncLogs()
	tm, cmd := m.listServerDatabases(c)
	next := tm.(model)
	next.dbIdx = pickDBIndex(next.databases, name)
	next.focus = paneDBs
	if !containsString(next.databases, name) {
		next.databases = append(next.databases, name)
		next.dbIdx = len(next.databases) - 1
		next.dbLists[c.ID()] = next.databases
	}
	return next, cmd
}

func containsString(items []string, want string) bool {
	for _, s := range items {
		if s == want {
			return true
		}
	}
	return false
}

func (m model) beginImport() (tea.Model, tea.Cmd) {
	if _, ok := m.currentConn(); !ok {
		return m.noticeAndLog("select a server first")
	}
	if _, ok := m.currentDB(); !ok {
		return m.noticeAndLog("select a database first")
	}
	if _, ok := m.currentFile(); !ok {
		return m.noticeAndLog("select a dump file first")
	}
	if m.runner.Running() {
		return m.noticeAndLog("a job is already running")
	}
	c, _ := m.currentConn()
	if _, src := m.passwordFor(c); src == "" {
		m.pending = pendingImport
		return m.beginPassword()
	}
	m.pending = pendingNone
	m.overlay = overlayConfirmImport
	return m, nil
}

func (m model) beginExport() (tea.Model, tea.Cmd) {
	c, ok := m.selectedTarget()
	if !ok {
		if _, sok := m.currentConn(); !sok {
			return m.noticeAndLog("select a server first")
		}
		return m.noticeAndLog("select a database first")
	}
	if m.runner.Running() {
		return m.noticeAndLog("a job is already running")
	}
	if _, src := m.passwordFor(c); src == "" {
		m.pending = pendingExport
		return m.beginPassword()
	}
	m.pending = pendingNone
	m.overlay = overlayExportPath
	m.pathInput.SetValue(defaultExportPath(c.Database))
	m.pathInput.CursorEnd()
	m.pathInput.Focus()
	return m, textinput.Blink
}

func (m model) startImportJob() (model, tea.Cmd) {
	c, ok := m.selectedTarget()
	if !ok {
		if _, sok := m.currentConn(); !sok {
			return m.failStart("select a server first")
		}
		return m.failStart("select a database first")
	}
	file, ok := m.currentFile()
	if !ok {
		return m.failStart("select a dump file first")
	}
	if err := c.ValidateFields(); err != nil {
		return m.failStart(err.Error())
	}
	if missing := missingTools(c, file, m.clearDB, false); len(missing) > 0 {
		return m.failStart("missing client tools: " + strings.Join(missing, ", "))
	}
	if m.runner.Running() {
		return m.failStart("a job is already running")
	}
	pw, _ := m.passwordFor(c)
	clear := m.clearDB
	cwd := m.cwd
	rel := file
	m = m.persistLastUsed(c)
	m.stickLog = true
	title := fmt.Sprintf("import %s -> %s", rel, c.Database)
	if clear {
		title += " (clear)"
	}
	if !m.runner.Start(title, func(ctx context.Context, log func(string)) error {
		path := rel
		if !filepath.IsAbs(path) {
			path = exportAbs(cwd, rel)
		}
		return importDB(ctx, c, pw, path, clear, log)
	}) {
		return m.failStart("a job is already running")
	}
	m.syncLogs()
	return m, nil
}

func (m model) startExportJob(path string) (model, tea.Cmd) {
	c, ok := m.selectedTarget()
	if !ok {
		if _, sok := m.currentConn(); !sok {
			return m.failStart("select a server first")
		}
		return m.failStart("select a database first")
	}
	if err := c.ValidateFields(); err != nil {
		return m.failStart(err.Error())
	}
	if path == "" {
		path = defaultExportPath(c.Database)
	}
	cwd := m.cwd
	out, err := resolveExportPath(cwd, path)
	if err != nil {
		return m.failStart(err.Error())
	}
	if missing := missingTools(c, "", false, true); len(missing) > 0 {
		return m.failStart("missing client tools: " + strings.Join(missing, ", "))
	}
	if m.runner.Running() {
		return m.failStart("a job is already running")
	}
	pw, _ := m.passwordFor(c)
	m = m.persistLastUsed(c)
	m.stickLog = true
	if !m.runner.Start("export "+c.Database+" -> "+path, func(ctx context.Context, log func(string)) error {
		return exportDB(ctx, c, pw, out, log)
	}) {
		return m.failStart("a job is already running")
	}
	m.syncLogs()
	return m, nil
}

func (m model) appendLog(s string) model {
	m.runner.Append(s)
	m.stickLog = true
	m.syncLogs()
	return m
}

func (m model) showNotice(s string) (tea.Model, tea.Cmd) {
	m.notice = s
	m.overlay = overlayNotice
	return m, nil
}

func (m model) noticeAndLog(s string) (tea.Model, tea.Cmd) {
	m = m.appendLog(s)
	return m.showNotice(s)
}

func (m model) failStart(s string) (model, tea.Cmd) {
	m = m.appendLog(s)
	m.notice = s
	m.overlay = overlayNotice
	return m, nil
}

func (m *model) sizeLogPane() {
	bodyH := max(1, m.height-2)
	_, _, _, _, logH := paneSizes(m.width, bodyH)
	frame := paneBorder(false)
	innerW := max(1, m.width-frame.GetHorizontalFrameSize())
	innerH := max(1, logH-frame.GetVerticalFrameSize())
	m.logVP.Width = innerW
	m.logVP.Height = max(1, innerH-1)
}

func (m *model) syncLogs() {
	if m.runner.Running() {
		m.stickLog = true
	}
	lines, _, _ := m.runner.Snapshot()
	content := strings.Join(lines, "\n")
	y := m.logVP.YOffset
	m.logVP.SetContent(content)
	if m.stickLog {
		m.logVP.GotoBottom()
		return
	}
	m.logVP.SetYOffset(y)
}

func (m *model) handleLogKeys(key string) {
	switch key {
	case "j", "down":
		m.stickLog = false
		m.logVP.LineDown(1)
	case "k", "up":
		m.stickLog = false
		m.logVP.LineUp(1)
	case "g", "home":
		m.stickLog = false
		m.logVP.GotoTop()
	case "G", "end":
		m.stickLog = true
		m.logVP.GotoBottom()
	case "pgdown", "ctrl+d":
		m.stickLog = false
		m.logVP.HalfViewDown()
	case "pgup", "ctrl+u":
		m.stickLog = false
		m.logVP.HalfViewUp()
	}
	if m.logVP.AtBottom() {
		m.stickLog = true
	}
}

func moveIndex(idx, n int, key string) int {
	if n == 0 {
		return 0
	}
	switch key {
	case "j", "down":
		if idx < n-1 {
			return idx + 1
		}
	case "k", "up":
		if idx > 0 {
			return idx - 1
		}
	case "g", "home":
		return 0
	case "G", "end":
		return n - 1
	}
	return idx
}

func indexByID(conns []config.Connection, id string) int {
	if id == "" {
		return -1
	}
	for i, c := range conns {
		if c.ID() == id {
			return i
		}
	}
	return -1
}
