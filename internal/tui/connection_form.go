package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	formEngine = iota
	formName
	formHost
	formPort
	formUser
	formPass
	formSaveKey
	formFieldCount
)

const (
	idxName = 0
	idxHost = 1
	idxPort = 2
	idxUser = 3
	idxPass = 4
)

func newFormInputs() []textinput.Model {
	mk := func(placeholder string, password bool) textinput.Model {
		ti := themeInput(textinput.New())
		ti.Placeholder = placeholder
		ti.Prompt = ""
		ti.CharLimit = 256
		if password {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
			ti.CharLimit = 512
		}
		return ti
	}
	return []textinput.Model{
		mk("optional", false),
		mk("127.0.0.1", false),
		mk("5432", false),
		mk("user", false),
		mk("", true),
	}
}

func (m *model) blurForm() {
	for i := range m.formInputs {
		m.formInputs[i].Blur()
	}
}

func (m *model) syncFormFocus() {
	m.blurForm()
	if i, ok := formInputIndex(m.formFocus); ok {
		m.formInputs[i].Focus()
	}
}

func formInputIndex(focus int) (int, bool) {
	switch focus {
	case formName:
		return idxName, true
	case formHost:
		return idxHost, true
	case formPort:
		return idxPort, true
	case formUser:
		return idxUser, true
	case formPass:
		return idxPass, true
	default:
		return 0, false
	}
}

func (m model) beginAdd() (tea.Model, tea.Cmd) {
	m.overlay = overlayAdd
	m.formEditID = ""
	m.formEngine = config.EnginePostgres
	m.formSaveKey = true
	m.formFocus = formEngine
	m.formInputs = newFormInputs()
	m.formInputs[idxHost].SetValue("127.0.0.1")
	m.formInputs[idxPort].SetValue(strconv.Itoa(config.DefaultPort(m.formEngine)))
	m.formInputs[idxPass].Placeholder = "leave empty for trust"
	m.formErr = ""
	m.syncFormFocus()
	return m, textinput.Blink
}

func (m model) beginEdit() (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m.showNotice("select a server first")
	}
	m.overlay = overlayEdit
	m.formEditID = c.ID()
	m.formEngine = c.Engine
	if m.formEngine == "" {
		m.formEngine = config.EnginePostgres
	}
	m.formSaveKey = true
	m.formFocus = formEngine
	m.formInputs = newFormInputs()
	m.formInputs[idxName].SetValue(c.Name)
	m.formInputs[idxHost].SetValue(c.Host)
	m.formInputs[idxPort].SetValue(strconv.Itoa(c.Port))
	m.formInputs[idxUser].SetValue(c.User)
	m.formInputs[idxPass].Placeholder = "leave empty to keep"
	m.formErr = ""
	m.syncFormFocus()
	return m, textinput.Blink
}

func (m model) beginDelete() (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	if !ok {
		return m.showNotice("select a server first")
	}
	if !c.Saved() {
		return m.showNotice("this connection comes from .env or compose; remove it there")
	}
	m.overlay = overlayConfirmDelete
	return m, nil
}

func (m *model) setFormEngine(e config.Engine) {
	old := m.formEngine
	m.formEngine = e
	cur := strings.TrimSpace(m.formInputs[idxPort].Value())
	if cur == "" || cur == strconv.Itoa(config.DefaultPort(old)) {
		m.formInputs[idxPort].SetValue(strconv.Itoa(config.DefaultPort(e)))
	}
}

func connectionFromForm(engine config.Engine, inputs []textinput.Model) (config.Connection, error) {
	name := strings.TrimSpace(inputs[idxName].Value())
	host := strings.TrimSpace(inputs[idxHost].Value())
	user := strings.TrimSpace(inputs[idxUser].Value())
	portStr := strings.TrimSpace(inputs[idxPort].Value())
	port, err := strconv.Atoi(portStr)
	if host == "" || user == "" || err != nil || port <= 0 {
		return config.Connection{}, fmt.Errorf("host, port, and user are required")
	}
	if name == "" {
		name = user
	}
	c := config.Connection{
		Name:   name,
		Engine: engine,
		Host:   host,
		Port:   port,
		User:   user,
		Source: config.SourceSaved,
	}
	if err := c.ValidateFields(); err != nil {
		return config.Connection{}, err
	}
	return c, nil
}

func (m model) saveConnForm() (tea.Model, tea.Cmd) {
	c, err := connectionFromForm(m.formEngine, m.formInputs)
	if err != nil {
		m.formErr = err.Error()
		return m, nil
	}
	oldID := m.formEditID
	newID := c.ID()
	pw := m.formInputs[idxPass].Value()
	oldPW := ""
	if oldID != "" {
		if oldID != newID {
			if got, err := m.storeOrDefault().Get(oldID); err == nil && got != "" {
				oldPW = got
			}
		}
		if oldPW == "" {
			oldPW, _ = m.passwordForID(oldID)
		}
	}

	if oldID != "" && oldID != newID {
		m.cfg.Remove(oldID)
		delete(m.memPW, oldID)
		delete(m.hasKey, oldID)
		delete(m.dbLists, oldID)
		delete(m.dbErrors, oldID)
		_ = m.storeOrDefault().Delete(oldID)
	}
	if pw == "" {
		pw = oldPW
	}
	c.NoPassword = pw == ""

	if old := m.connByID(oldID); old.LastDatabase != "" {
		c.LastDatabase = old.LastDatabase
	}

	m.cfg.Upsert(c)
	m.cfg.LastUsed = newID
	save := m.saveCfg
	if save == nil {
		save = config.Save
	}
	if err := save(m.cfg); err != nil {
		m.formErr = "save config: " + err.Error()
		return m, nil
	}

	m.memPW[newID] = pw
	if pw == "" {
		m.hasKey[newID] = false
		_ = m.storeOrDefault().Delete(newID)
	} else if m.formSaveKey {
		if err := m.storeOrDefault().Set(newID, pw); err != nil {
			m.hasKey[newID] = false
			m = m.appendLog("keychain save failed: " + err.Error() + " (using this session only)")
		} else {
			m.hasKey[newID] = true
		}
	}

	m = m.replaceConn(oldID, c)
	m.blurForm()
	m.formErr = ""
	m.stickLog = true
	m = m.appendLog("saved " + c.Short())
	testErr := testConn(context.Background(), c, pw, m.runner.Append)
	m.syncLogs()
	msg := "saved " + c.Short()
	if testErr != nil {
		m.databases = nil
		m.dbIdx = 0
		m.dbReady = false
		m.dbErr = testErr.Error()
		delete(m.dbLists, newID)
		m.dbErrors[newID] = m.dbErr
		msg += " · connection failed: " + testErr.Error()
		m = m.appendLog(msg)
		m.notice = msg
		m.overlay = overlayNotice
		return m, nil
	}
	names, listErr := listDBs(context.Background(), c, pw, m.runner.Append)
	m.syncLogs()
	if listErr != nil {
		m.databases = nil
		m.dbIdx = 0
		m.dbReady = false
		m.dbErr = listErr.Error()
		delete(m.dbLists, newID)
		m.dbErrors[newID] = m.dbErr
		msg += " · connected · list failed: " + listErr.Error()
		m = m.appendLog(msg)
		m.notice = msg
		m.overlay = overlayNotice
		return m, nil
	}
	m.dbErr = ""
	m.databases = names
	m.dbReady = true
	m.dbIdx = pickDBIndex(names, c.LastDatabase, c.Database)
	m.dbLists[newID] = names
	delete(m.dbErrors, newID)
	msg += " · connected"
	m = m.appendLog(msg)
	m.notice = msg
	m.overlay = overlayNotice
	return m, nil
}

func (m model) connByID(id string) config.Connection {
	if i := indexByID(m.conns, id); i >= 0 {
		return m.conns[i]
	}
	return config.Connection{}
}

func (m model) passwordForID(id string) (string, string) {
	return m.lookupPassword(id, m.connByID(id).NoPassword)
}

func (m model) replaceConn(oldID string, c config.Connection) model {
	newID := c.ID()
	next := make([]config.Connection, 0, len(m.conns)+1)
	seen := false
	for _, existing := range m.conns {
		if existing.ID() == oldID || existing.ID() == newID {
			if !seen {
				next = append(next, c)
				seen = true
			}
			continue
		}
		next = append(next, existing)
	}
	if !seen {
		next = append(next, c)
	}
	m.conns = next
	m.connIdx = indexByID(m.conns, newID)
	if m.connIdx < 0 {
		m.connIdx = 0
	}
	return m
}

func (m model) confirmDelete() (tea.Model, tea.Cmd) {
	c, ok := m.currentConn()
	m.overlay = overlayNone
	if !ok || !c.Saved() {
		return m, nil
	}
	id := c.ID()
	m.cfg.Remove(id)
	if m.cfg.LastUsed == id {
		m.cfg.LastUsed = ""
	}
	save := m.saveCfg
	if save == nil {
		save = config.Save
	}
	_ = save(m.cfg)
	_ = m.storeOrDefault().Delete(id)
	delete(m.memPW, id)
	delete(m.hasKey, id)
	delete(m.dbLists, id)
	delete(m.dbErrors, id)

	next := make([]config.Connection, 0, len(m.conns))
	for _, existing := range m.conns {
		if existing.ID() != id {
			next = append(next, existing)
		}
	}
	m.conns = next
	if m.connIdx >= len(m.conns) {
		m.connIdx = len(m.conns) - 1
	}
	if m.connIdx < 0 {
		m.connIdx = 0
	}
	m.showDBsForCurrent()
	return m, nil
}

func (m model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
		m.blurForm()
		return m, nil
	case "enter":
		return m.saveConnForm()
	case "tab":
		m.formFocus = (m.formFocus + 1) % formFieldCount
		m.syncFormFocus()
		return m, textinput.Blink
	case "shift+tab":
		m.formFocus = (m.formFocus + formFieldCount - 1) % formFieldCount
		m.syncFormFocus()
		return m, textinput.Blink
	}

	if m.formFocus == formEngine {
		switch msg.String() {
		case " ", "space":
			if m.formEngine == config.EnginePostgres {
				m.setFormEngine(config.EngineMySQL)
			} else {
				m.setFormEngine(config.EnginePostgres)
			}
		case "p", "P":
			m.setFormEngine(config.EnginePostgres)
		case "m", "M":
			m.setFormEngine(config.EngineMySQL)
		}
		return m, nil
	}

	if m.formFocus == formSaveKey {
		switch msg.String() {
		case " ", "space", "y", "Y":
			m.formSaveKey = true
		case "n", "N":
			m.formSaveKey = false
		}
		return m, nil
	}

	i, ok := formInputIndex(m.formFocus)
	if !ok {
		return m, nil
	}
	var cmd tea.Cmd
	m.formInputs[i], cmd = m.formInputs[i].Update(msg)
	return m, cmd
}
