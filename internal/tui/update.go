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
	importDB = db.Import
	exportDB = db.Export
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
		m.focus = (m.focus + 1) % 3
		return m, nil
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
		return m, nil
	case "h", "left":
		if m.focus > paneConns {
			m.focus--
		}
		return m, nil
	case "l", "right":
		if m.focus < paneLogs {
			m.focus++
		}
		return m, nil
	case "?":
		m.overlay = overlayHelp
		return m, nil
	case "r":
		return m, reload(m.cwd)
	case "c":
		m.clearDB = !m.clearDB
		return m, nil
	case "p":
		return m.beginPassword()
	case "i":
		return m.beginImport()
	case "e":
		return m.beginExport()
	case "enter":
		if m.focus == paneFiles {
			return m.beginImport()
		}
		if m.focus == paneConns {
			if c, ok := m.currentConn(); ok {
				m = m.persistLastUsed(c)
				if _, src := m.passwordFor(c); src == "" {
					return m.beginPassword()
				}
			}
		}
		return m, nil
	}

	switch m.focus {
	case paneConns:
		m.connIdx = moveIndex(m.connIdx, len(m.conns), msg.String())
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
	}
	return m, nil
}

func (m model) beginPassword() (tea.Model, tea.Cmd) {
	if _, ok := m.currentConn(); !ok {
		return m.showNotice("select a connection first")
	}
	m.overlay = overlayPassword
	m.pwInput.SetValue("")
	m.pwInput.Focus()
	return m, textinput.Blink
}

func (m model) savePassword(persist bool) (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	m.overlay = overlayNone
	if !ok {
		return m, nil
	}
	pw := m.pwInput.Value()
	m.memPW[c.ID()] = pw
	if persist {
		if err := m.storeOrDefault().Set(c.ID(), pw); err != nil {
			return m.showNotice("keychain save failed: " + err.Error() + " (using this session only)")
		}
		m.hasKey[c.ID()] = true
		m = m.persistLastUsed(c)
	}
	return m, nil
}

func (m model) beginImport() (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m.showNotice("select a connection first")
	}
	if _, ok := m.currentFile(); !ok {
		return m.showNotice("select a dump file first")
	}
	if m.runner.Running() {
		return m.showNotice("a job is already running")
	}
	if _, src := m.passwordFor(c); src == "" {
		return m.beginPassword()
	}
	m.overlay = overlayConfirmImport
	return m, nil
}

func (m model) beginExport() (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m.showNotice("select a connection first")
	}
	if m.runner.Running() {
		return m.showNotice("a job is already running")
	}
	if _, src := m.passwordFor(c); src == "" {
		return m.beginPassword()
	}
	m.overlay = overlayExportPath
	m.pathInput.SetValue(defaultExportPath(c))
	m.pathInput.CursorEnd()
	m.pathInput.Focus()
	return m, textinput.Blink
}

func (m model) startImportJob() (model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m, nil
	}
	file, ok := m.currentFile()
	if !ok {
		return m, nil
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
	m.runner.Start(title, func(ctx context.Context, log func(string)) error {
		path := rel
		if !filepath.IsAbs(path) {
			path = exportAbs(cwd, rel)
		}
		return importDB(ctx, c, pw, path, clear, log)
	})
	return m, nil
}

func (m model) startExportJob(path string) (model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m, nil
	}
	if path == "" {
		path = defaultExportPath(c)
	}
	pw, _ := m.passwordFor(c)
	cwd := m.cwd
	m = m.persistLastUsed(c)
	m.stickLog = true
	out := exportAbs(cwd, path)
	m.runner.Start("export "+c.Database+" -> "+path, func(ctx context.Context, log func(string)) error {
		return exportDB(ctx, c, pw, out, log)
	})
	return m, nil
}

func (m model) showNotice(s string) (tea.Model, tea.Cmd) {
	m.notice = s
	m.overlay = overlayNotice
	return m, nil
}

func (m *model) sizeLogPane() {
	_, _, logW, logH := paneSizes(m.width, m.height)
	m.logVP.Width = max(1, logW-2)
	m.logVP.Height = max(1, logH-2)
}

func (m *model) syncLogs() {
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
