package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
	"github.com/AmirSyafiq2112/lazydbm/internal/db"
	"github.com/AmirSyafiq2112/lazydbm/internal/discover"
	"github.com/AmirSyafiq2112/lazydbm/internal/secret"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	importDB       = db.Import
	resetSchema    = db.ResetSchema
	exportDB       = db.Export
	listDBs        = db.ListDatabases
	listSchemas    = db.ListSchemas
	testConn       = db.TestConnection
	missingTools   = db.MissingTools
	ensureDB       = db.EnsureDatabase
	exportSchema   = db.ExportSchema
	exportSchemas  = db.ExportSchemas
	previewSchemas = db.PreviewSchemaExport
	dumpSchemas    = db.SchemasInFile
	listTables     = db.ListTables
	previewTables  = db.PreviewTableExport
	exportTableSet = db.ExportTables
	dumpTables     = db.TablesInFile
	resetTables    = db.ResetTables
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
		selected, _ := m.currentFile()
		m.cfg = msg.cfg
		m.conns = msg.res.Connections
		m.files = msg.res.Files
		m.selectFile(selected)
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
	if m.fileSearch {
		if msg.String() == "ctrl+c" {
			if m.runner.Running() {
				m.runner.Cancel()
				return m, nil
			}
			return m, tea.Quit
		}
		return m.handleFileSearch(msg)
	}
	if m.dbSearch {
		if msg.String() == "ctrl+c" {
			if m.runner.Running() {
				m.runner.Cancel()
				return m, nil
			}
			return m, tea.Quit
		}
		return m.handleDBSearch(msg)
	}
	if m.tableSearch {
		if msg.String() == "ctrl+c" {
			if m.runner.Running() {
				m.runner.Cancel()
				return m, nil
			}
			return m, tea.Quit
		}
		return m.handleTableSearch(msg)
	}
	if m.logFull {
		return m.handleLogFullKey(msg)
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
	case "f":
		m.openLogFull()
		return m, nil
	case "/":
		if m.focus == paneFiles {
			m.fileSearch = true
			return m, nil
		}
		if m.focus == paneDBs {
			if m.tableMode {
				m.tableSearch = true
				return m, nil
			}
			m.dbSearch = true
			return m, nil
		}
	case "r":
		return m.refreshAll()
	case "c":
		m.clearDB = !m.clearDB
		return m, nil
	case "p":
		return m.beginPassword()
	case "P":
		return m.toggleProtected()
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
		if m.focus == paneDBs {
			if m.tableMode {
				return m.beginExport()
			}
			if m.schemaMode {
				return m.openTables()
			}
			c, _ := m.currentConn()
			if c.Engine == config.EngineMySQL {
				return m.openTables()
			}
			return m.openSchemas()
		}
		return m, nil
	case "backspace", "ctrl+h":
		if m.tableMode {
			m.clearTables()
			return m, nil
		}
		if m.schemaMode {
			m.clearSchemas()
			return m, nil
		}
	}

	switch m.focus {
	case paneServers:
		prev := m.connIdx
		m.connIdx = moveIndex(m.connIdx, len(m.conns), msg.String())
		if m.connIdx != prev {
			m.showDBsForCurrent()
		}
	case paneDBs:
		if m.tableMode {
			m.tableIdx = moveIndex(m.tableIdx, len(m.visibleTables()), msg.String())
		} else if m.schemaMode {
			m.schemaIdx = moveIndex(m.schemaIdx, len(m.visibleSchemas()), msg.String())
		} else if m.dbReady && m.dbErr == "" {
			m.dbIdx = moveIndex(m.dbIdx, len(m.visibleDatabases()), msg.String())
		}
	case paneFiles:
		m.fileIdx = moveIndex(m.fileIdx, len(m.visibleFiles()), msg.String())
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
			if m.targetProtected() {
				return m.beginProtectedImport()
			}
			m.overlay = overlayNone
			return m.startImportJob()
		}
		return m, nil

	case overlayProtectedImport:
		switch msg.String() {
		case "esc":
			m.overlay = overlayNone
			m.guardErr = ""
			m.dbInput.Blur()
			return m, nil
		case "enter":
			name := m.importDatabaseName()
			if strings.TrimSpace(m.dbInput.Value()) != name {
				m.guardErr = "type " + name + " to continue"
				return m, nil
			}
			m.overlay = overlayNone
			m.guardErr = ""
			m.dbInput.Blur()
			return m.startImportJob()
		}
		m.guardErr = ""
		var cmd tea.Cmd
		m.dbInput, cmd = m.dbInput.Update(msg)
		return m, cmd

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

	case overlayExportPath, overlayExportPreview:
		switch msg.String() {
		case "esc":
			m.overlay = overlayNone
			m.exportPlan = nil
			m.exportTables = nil
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
		return m.openImportConfirm()
	case pendingExport:
		m.pending = pendingNone
		return m.beginExport()
	case pendingSchemas:
		m.pending = pendingNone
		return m.openSchemas()
	case pendingTables:
		m.pending = pendingNone
		return m.openTables()
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
	selected, _ := m.currentFile()
	files, err := discover.Files(m.cwd)
	if err != nil {
		return m.noticeAndLog("refresh files: " + err.Error())
	}
	m.files = files
	m.selectFile(selected)
	m = m.appendLog(fmt.Sprintf("refreshed %d sql files", len(m.files)))
	cmd := reload(m.cwd)
	c, ok := m.currentConn()
	if !ok {
		return m, cmd
	}
	if _, src := m.passwordFor(c); src == "" {
		return m, cmd
	}
	wasSchema := m.schemaMode
	wasTable := m.tableMode
	tableSchema := m.tableSchema
	tm, _ := m.listServerDatabases(c)
	next, ok := tm.(model)
	if !ok || next.overlay != overlayNone {
		return tm, cmd
	}
	if wasSchema && next.schemaDB != "" {
		opened, _ := next.openSchemas()
		next, _ = opened.(model)
	}
	if wasTable && next.overlay == overlayNone {
		if tableSchema != "" {
			for i, name := range next.schemas {
				if name == tableSchema {
					next.schemaIdx = i
					break
				}
			}
		}
		opened, _ := next.openTables()
		return opened, cmd
	}
	if wasSchema {
		return next, cmd
	}
	return tm, cmd
}

func (m model) listServerDatabases(c config.Connection) (tea.Model, tea.Cmd) {
	pw, _ := m.passwordFor(c)
	id := c.ID()
	m.stickLog = true
	m = m.appendLog("connect " + c.Short())
	if err := testConn(context.Background(), c, pw, m.runner.Append); err != nil {
		m.clearSchemas()
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
	m.clearDBSearch()
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
	m.clearDBSearch()
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

func indexOfName(items []string, want string) int {
	if i, ok := nameIndex(items, want); ok {
		return i
	}
	return 0
}

func nameIndex(items []string, want string) (int, bool) {
	if want == "" {
		return 0, false
	}
	for i, name := range items {
		if name == want {
			return i, true
		}
	}
	return 0, false
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
	if m.tableMode {
		if _, ok := m.currentTable(); !ok {
			return m.noticeAndLog("select a table first")
		}
	} else if m.schemaMode {
		if _, ok := m.currentSchema(); !ok {
			return m.noticeAndLog("select a schema first")
		}
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
	return m.openImportConfirm()
}

func (m model) openImportConfirm() (tea.Model, tea.Cmd) {
	m.importSchemas = nil
	m.importTables = nil
	file, ok := m.currentFile()
	if !ok {
		return m.noticeAndLog("select a dump file first")
	}
	path := file
	if !filepath.IsAbs(path) {
		path = exportAbs(m.cwd, file)
	}
	if m.tableMode {
		tables, err := dumpTables(path)
		if err != nil {
			return m.noticeAndLog("read dump tables: " + err.Error())
		}
		m.importTables = tables
		if len(tables) == 0 {
			m = m.appendLog("no CREATE TABLE in " + filepath.Base(file) + "; clear drops the selected table")
		} else {
			labels := make([]string, len(tables))
			for i, table := range tables {
				labels[i] = table.Label()
			}
			m = m.appendLog(fmt.Sprintf("%s creates %d tables: %s", filepath.Base(file), len(tables), strings.Join(labels, ", ")))
		}
	} else if m.schemaMode {
		names, err := dumpSchemas(path)
		if err != nil {
			return m.noticeAndLog("read dump schemas: " + err.Error())
		}
		m.importSchemas = names
		if len(names) == 0 {
			m = m.appendLog("no CREATE SCHEMA in " + filepath.Base(file) + "; clear drops the selected schema")
		} else {
			m = m.appendLog(fmt.Sprintf("%s creates %d schemas: %s", filepath.Base(file), len(names), strings.Join(names, ", ")))
		}
	}
	m.overlay = overlayConfirmImport
	return m, nil
}

func (m model) openSchemas() (tea.Model, tea.Cmd) {
	c, ok := m.selectedTarget()
	if !ok {
		if _, sok := m.currentConn(); !sok {
			return m.noticeAndLog("select a server first")
		}
		return m.noticeAndLog("select a database first")
	}
	if c.Engine != config.EnginePostgres {
		return m.noticeAndLog("mysql export uses the whole database")
	}
	if _, src := m.passwordFor(c); src == "" {
		m.pending = pendingSchemas
		return m.beginPassword()
	}
	pw, _ := m.passwordFor(c)
	m.stickLog = true
	m = m.appendLog("listing schemas in " + c.Database)
	names, err := listSchemas(context.Background(), c, pw, m.runner.Append)
	m.syncLogs()
	if err != nil {
		return m.noticeAndLog("list schemas: " + err.Error())
	}
	m.clearDBSearch()
	m.clearTables()
	m.schemaMode = true
	m.schemaDB = c.Database
	m.schemas = names
	m.schemaIdx = 0
	m.focus = paneDBs
	return m.appendLog(fmt.Sprintf("listed %d schemas", len(names))), nil
}

func (m model) openTables() (tea.Model, tea.Cmd) {
	c, ok := m.selectedTarget()
	if !ok {
		if _, sok := m.currentConn(); !sok {
			return m.noticeAndLog("select a server first")
		}
		return m.noticeAndLog("select a database first")
	}
	schema := ""
	if c.Engine == config.EnginePostgres {
		var sok bool
		schema, sok = m.currentSchema()
		if !sok {
			return m.noticeAndLog("select a schema first")
		}
		if m.schemaDB != "" {
			c = c.WithDatabase(m.schemaDB)
		}
	}
	if _, src := m.passwordFor(c); src == "" {
		m.pending = pendingTables
		return m.beginPassword()
	}
	pw, _ := m.passwordFor(c)
	m.stickLog = true
	where := c.Database
	if schema != "" {
		where = c.Database + "." + schema
	}
	m = m.appendLog("listing tables in " + where)
	names, err := listTables(context.Background(), c, pw, schema, m.runner.Append)
	m.syncLogs()
	if err != nil {
		return m.noticeAndLog("list tables: " + err.Error())
	}
	m.clearDBSearch()
	m.tableMode = true
	m.tableSchema = schema
	m.tableDB = c.Database
	m.tables = names
	m.tableIdx = 0
	m.tableQuery = ""
	m.tableSearch = false
	m.focus = paneDBs
	return m.appendLog(fmt.Sprintf("listed %d tables", len(names))), nil
}

func (m model) beginExport() (tea.Model, tea.Cmd) {
	c, ok := m.selectedTarget()
	if !ok {
		if _, sok := m.currentConn(); !sok {
			return m.noticeAndLog("select a server first")
		}
		return m.noticeAndLog("select a database first")
	}
	schema := ""
	table := ""
	if m.tableMode {
		var tok bool
		table, tok = m.currentTable()
		if !tok {
			return m.noticeAndLog("select a table first")
		}
		schema = m.tableSchema
		if m.tableDB != "" {
			c = c.WithDatabase(m.tableDB)
		}
	} else if m.schemaMode {
		var sok bool
		schema, sok = m.currentSchema()
		if !sok {
			return m.noticeAndLog("select a schema first")
		}
		if m.schemaDB != "" {
			c = c.WithDatabase(m.schemaDB)
		}
	}
	if m.runner.Running() {
		return m.noticeAndLog("a job is already running")
	}
	if _, src := m.passwordFor(c); src == "" {
		m.pending = pendingExport
		return m.beginPassword()
	}
	m.pending = pendingNone
	m.exportSchema = schema
	m.exportPlan = nil
	m.exportTables = nil
	label := c.Database
	if schema != "" {
		label = c.Database + "-" + schema
	}
	if table != "" {
		if schema != "" {
			label = c.Database + "-" + schema + "-" + table
		} else {
			label = c.Database + "-" + table
		}
		pw, _ := m.passwordFor(c)
		m = m.appendLog("checking tables related to " + table)
		plan, err := previewTables(context.Background(), c, pw, schema, table, m.runner.Append)
		m.syncLogs()
		if err != nil {
			return m.noticeAndLog("table preview: " + err.Error())
		}
		m.exportTables = plan
		m.overlay = overlayExportPreview
	} else if schema != "" {
		pw, _ := m.passwordFor(c)
		m = m.appendLog("checking schemas related to " + schema)
		plan, err := previewSchemas(context.Background(), c, pw, schema, m.runner.Append)
		m.syncLogs()
		if err != nil {
			return m.noticeAndLog("schema preview: " + err.Error())
		}
		m.exportPlan = plan
		m.overlay = overlayExportPreview
	} else {
		m.overlay = overlayExportPath
	}
	m.pathInput.SetValue(defaultExportPath(label))
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
	schema := ""
	table := ""
	if m.tableMode {
		table, ok = m.currentTable()
		if !ok {
			return m.failStart("select a table first")
		}
		schema = m.tableSchema
		if m.tableDB != "" {
			c = c.WithDatabase(m.tableDB)
		}
	} else if m.schemaMode {
		schema, ok = m.currentSchema()
		if !ok {
			return m.failStart("select a schema first")
		}
		if m.schemaDB != "" {
			c = c.WithDatabase(m.schemaDB)
		}
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
	clearTables := append([]db.TableRef(nil), m.importTables...)
	clearNames := append([]string(nil), m.importSchemas...)
	if m.tableMode {
		clearNames = nil
		if !clear {
			clearTables = nil
		} else if len(clearTables) == 0 && table != "" {
			clearTables = []db.TableRef{{Schema: schema, Name: table}}
		}
	} else {
		clearTables = nil
		if clear && schema != "" && len(clearNames) == 0 {
			clearNames = []string{schema}
		}
	}
	m = m.persistLastUsed(c)
	m.stickLog = true
	title := fmt.Sprintf("import %s -> %s", rel, c.Database)
	if m.tableMode && table != "" && len(clearTables) <= 1 {
		shown := table
		if schema != "" {
			shown = schema + "." + table
		}
		title = fmt.Sprintf("import %s -> %s.%s", rel, c.Database, shown)
	} else if schema != "" && len(clearNames) <= 1 && !m.tableMode {
		shown := schema
		if len(clearNames) == 1 {
			shown = clearNames[0]
		}
		title = fmt.Sprintf("import %s -> %s.%s", rel, c.Database, shown)
	}
	if clear {
		switch {
		case len(clearTables) > 0:
			labels := make([]string, len(clearTables))
			for i, item := range clearTables {
				labels[i] = item.Label()
			}
			title += " (clear " + strings.Join(labels, ", ") + ")"
		case len(clearNames) > 0:
			title += " (clear " + strings.Join(clearNames, ", ") + ")"
		default:
			title += " (clear)"
		}
	}
	if !m.runner.Start(title, func(ctx context.Context, log func(string)) error {
		path := rel
		if !filepath.IsAbs(path) {
			path = exportAbs(cwd, rel)
		}
		if clear && len(clearTables) > 0 {
			if err := resetTables(ctx, c, pw, clearTables, log); err != nil {
				return err
			}
			return importDB(ctx, c, pw, path, false, log)
		}
		if clear && len(clearNames) > 0 {
			for _, name := range clearNames {
				if err := resetSchema(ctx, c, pw, name, log); err != nil {
					return err
				}
			}
			return importDB(ctx, c, pw, path, false, log)
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
	schema := m.exportSchema
	plan := append([]db.SchemaStat(nil), m.exportPlan...)
	tables := append([]db.TableStat(nil), m.exportTables...)
	m.exportPlan = nil
	m.exportTables = nil
	if len(tables) > 0 && m.tableDB != "" {
		c = c.WithDatabase(m.tableDB)
	} else if schema != "" && m.schemaDB != "" {
		c = c.WithDatabase(m.schemaDB)
	}
	m = m.persistLastUsed(c)
	m.stickLog = true
	title := "export " + c.Database + " -> " + path
	names := make([]string, 0, len(plan))
	for _, s := range plan {
		names = append(names, s.Name)
	}
	if len(tables) > 0 {
		title = "export " + c.Database + " " + tables[0].Label() + " -> " + path
		if len(tables) > 1 {
			title = fmt.Sprintf("export %s %s + %d tables -> %s", c.Database, tables[0].Label(), len(tables)-1, path)
		}
	} else if schema != "" {
		title = "export " + c.Database + "." + schema + " -> " + path
		if len(names) > 1 {
			title = fmt.Sprintf("export %s.%s + %d schemas -> %s", c.Database, schema, len(names)-1, path)
		}
	}
	if !m.runner.Start(title, func(ctx context.Context, log func(string)) error {
		if len(tables) > 0 {
			return exportTableSet(ctx, c, pw, tables, out, log)
		}
		if len(names) > 0 {
			return exportSchemas(ctx, c, pw, names, out, log)
		}
		if schema != "" {
			return exportSchema(ctx, c, pw, schema, out, log)
		}
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

func (m *model) openLogFull() {
	m.logFull = true
	m.sizeLogPane()
	m.syncLogs()
}

func (m *model) closeLogFull() {
	m.logFull = false
	m.sizeLogPane()
	m.syncLogs()
}

func (m model) handleLogFullKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "esc", "f":
		m.closeLogFull()
		return m, nil
	default:
		m.handleLogKeys(msg.String())
		return m, nil
	}
}

func (m model) toggleProtected() (tea.Model, tea.Cmd) {
	if m.focus != paneDBs || m.schemaMode || m.tableMode {
		return m, nil
	}
	dbName, ok := m.currentDB()
	if !ok {
		return m.noticeAndLog("select a database first")
	}
	c, ok := m.currentConn()
	if !ok {
		return m.noticeAndLog("select a server first")
	}
	i := indexByID(m.conns, c.ID())
	if i < 0 || !m.conns[i].Saved() {
		return m.noticeAndLog("save this server before marking a database protected")
	}
	on := !m.conns[i].Protects(dbName)
	m.conns[i].SetProtected(dbName, on)
	m.cfg.Upsert(m.conns[i])
	save := m.saveCfg
	if save == nil {
		save = config.Save
	}
	if err := save(m.cfg); err != nil {
		return m.noticeAndLog("save config: " + err.Error())
	}
	if on {
		return m.appendLog(dbName + " is protected"), nil
	}
	return m.appendLog(dbName + " is no longer protected"), nil
}

func (m model) targetProtected() bool {
	c, ok := m.currentConn()
	if !ok {
		return false
	}
	if i := indexByID(m.conns, c.ID()); i >= 0 {
		c = m.conns[i]
	}
	name := m.importDatabaseName()
	if name == "" {
		return false
	}
	return c.Protects(name)
}

func (m model) importDatabaseName() string {
	if m.tableDB != "" {
		return m.tableDB
	}
	if m.schemaDB != "" {
		return m.schemaDB
	}
	name, _ := m.currentDB()
	return name
}

func (m model) beginProtectedImport() (tea.Model, tea.Cmd) {
	name := m.importDatabaseName()
	m.overlay = overlayProtectedImport
	m.guardErr = ""
	m.dbInput.SetValue("")
	m.dbInput.Placeholder = name
	m.dbInput.Focus()
	return m, textinput.Blink
}

func (m model) handleDBSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		selected := m.dbSearchSelection()
		m.dbSearch = false
		m.dbQuery = ""
		m.followDBSearch(selected)
		return m, nil
	case "enter":
		m.dbSearch = false
		m.followDBSearch(m.dbSearchSelection())
		return m, nil
	case "backspace", "ctrl+h":
		selected := m.dbSearchSelection()
		runes := []rune(m.dbQuery)
		if len(runes) > 0 {
			m.dbQuery = string(runes[:len(runes)-1])
		}
		m.followDBSearch(selected)
		return m, nil
	case "ctrl+u":
		selected := m.dbSearchSelection()
		m.dbQuery = ""
		m.followDBSearch(selected)
		return m, nil
	}
	if r, ok := searchRune(msg); ok {
		selected := m.dbSearchSelection()
		m.dbQuery += string(r)
		m.followDBSearch(selected)
	}
	return m, nil
}

func (m model) dbSearchSelection() string {
	names := m.visibleDatabases()
	idx := m.dbIdx
	if m.schemaMode {
		names = m.visibleSchemas()
		idx = m.schemaIdx
	}
	if idx < 0 || idx >= len(names) {
		return ""
	}
	return names[idx]
}

func (m *model) followDBSearch(selected string) {
	names := m.visibleDatabases()
	idx := &m.dbIdx
	if m.schemaMode {
		names = m.visibleSchemas()
		idx = &m.schemaIdx
	}
	if i, ok := nameIndex(names, selected); ok {
		*idx = i
		return
	}
	*idx = 0
}

func (m model) handleTableSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.tableSearch = false
		m.tableQuery = ""
		m.tableIdx = 0
		return m, nil
	case "enter":
		m.tableSearch = false
		if m.tableIdx >= len(m.visibleTables()) {
			m.tableIdx = 0
		}
		return m, nil
	case "backspace", "ctrl+h":
		runes := []rune(m.tableQuery)
		if len(runes) > 0 {
			m.tableQuery = string(runes[:len(runes)-1])
			if m.tableIdx >= len(m.visibleTables()) {
				m.tableIdx = 0
			}
		}
		return m, nil
	case "ctrl+u":
		m.tableQuery = ""
		m.tableIdx = 0
		return m, nil
	}
	if r, ok := searchRune(msg); ok {
		m.tableQuery += string(r)
		if m.tableIdx >= len(m.visibleTables()) {
			m.tableIdx = 0
		}
	}
	return m, nil
}

func (m model) handleFileSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.fileSearch = false
		m.fileQuery = ""
		m.fileIdx = 0
		return m, nil
	case "enter":
		m.fileSearch = false
		m.clampFileIdx()
		return m, nil
	case "backspace", "ctrl+h":
		if m.fileQuery != "" {
			m.fileQuery = m.fileQuery[:len(m.fileQuery)-1]
			m.clampFileIdx()
		}
		return m, nil
	case "ctrl+u":
		m.fileQuery = ""
		m.fileIdx = 0
		return m, nil
	}
	if r, ok := searchRune(msg); ok {
		m.fileQuery += string(r)
		m.clampFileIdx()
	}
	return m, nil
}

func searchRune(msg tea.KeyMsg) (rune, bool) {
	if msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace {
		return 0, false
	}
	s := msg.String()
	runes := []rune(s)
	if len(runes) != 1 {
		return 0, false
	}
	r := runes[0]
	if r < 32 || r == 127 {
		return 0, false
	}
	return r, true
}

func (m *model) sizeLogPane() {
	bodyH := max(1, m.height-2)
	var logH int
	if m.logFull {
		logH = bodyH
	} else {
		_, _, _, _, logH = paneSizes(m.width, bodyH)
	}
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
