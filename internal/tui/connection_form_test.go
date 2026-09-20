package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
	"github.com/AmirSyafiq2112/lazydbm/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
)

func fillForm(m model, name, host, port, user, pass string) model {
	m.formInputs[idxName].SetValue(name)
	m.formInputs[idxHost].SetValue(host)
	m.formInputs[idxPort].SetValue(port)
	m.formInputs[idxUser].SetValue(user)
	m.formInputs[idxPass].SetValue(pass)
	return m
}

func TestConnectionFromFormValidation(t *testing.T) {
	inputs := newFormInputs()
	if _, err := connectionFromForm(config.EnginePostgres, inputs); err == nil {
		t.Fatal("empty form should fail")
	}
	inputs[idxHost].SetValue("127.0.0.1")
	inputs[idxPort].SetValue("5432")
	inputs[idxUser].SetValue("app")
	c, err := connectionFromForm(config.EnginePostgres, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "app" || c.Database != "" || c.Source != config.SourceSaved || !c.Saved() {
		t.Fatalf("%#v", c)
	}
	inputs[idxName].SetValue("staging")
	c, err = connectionFromForm(config.EngineMySQL, inputs)
	if err != nil || c.Name != "staging" || c.Engine != config.EngineMySQL {
		t.Fatal(c, err)
	}
	inputs[idxPort].SetValue("nope")
	if _, err := connectionFromForm(config.EnginePostgres, inputs); err == nil {
		t.Fatal("bad port")
	}
	inputs[idxPort].SetValue("5432")
	inputs[idxUser].SetValue("--evil")
	if _, err := connectionFromForm(config.EnginePostgres, inputs); err == nil {
		t.Fatal("user --evil must fail ValidateFields")
	}
}

func TestSetFormEngineDefaultPort(t *testing.T) {
	m := testModel(t)
	m.formInputs = newFormInputs()
	m.formEngine = config.EnginePostgres
	m.formInputs[idxPort].SetValue("5432")
	m.setFormEngine(config.EngineMySQL)
	if m.formEngine != config.EngineMySQL || m.formInputs[idxPort].Value() != "3306" {
		t.Fatal(m.formEngine, m.formInputs[idxPort].Value())
	}
	m.formInputs[idxPort].SetValue("5433")
	m.setFormEngine(config.EnginePostgres)
	if m.formInputs[idxPort].Value() != "5433" {
		t.Fatal("custom port should be kept")
	}
}

func TestBeginAddAndSavePersistsWithoutPassword(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }
	tm, _ := m.beginAdd()
	m = asModel(t, tm)
	if m.overlay != overlayAdd {
		t.Fatal(m.overlay)
	}
	m.formEngine = config.EnginePostgres
	m = fillForm(m, "local", "127.0.0.1", "5432", "app", "s3cret")
	m.formSaveKey = true
	tm, _ = m.saveConnForm()
	m = asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, "saved") || !strings.Contains(m.notice, "connected") {
		t.Fatal(m.overlay, m.notice)
	}
	if len(saved.Connections) != 1 || saved.Connections[0].Source != config.SourceSaved {
		t.Fatalf("%#v", saved)
	}
	if saved.Connections[0].Database != "" {
		t.Fatalf("saved server must omit database: %#v", saved.Connections[0])
	}
	raw, err := yaml.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("password leaked into yaml: %s", raw)
	}
	c := saved.Connections[0]
	if m.memPW[c.ID()] != "s3cret" {
		t.Fatal("session password")
	}
	got, err := m.store.Get(c.ID())
	if err != nil || got != "s3cret" {
		t.Fatal(got, err)
	}
	if !strings.Contains(m.connLines()[indexByID(m.conns, c.ID())], "[saved]") {
		t.Fatal(m.connLines())
	}
	if !m.dbReady || len(m.databases) != 2 {
		t.Fatalf("save should list databases: %#v", m.databases)
	}
}

func TestAddEmptyPasswordSetsNoPassword(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }
	tm, _ := m.beginAdd()
	m = asModel(t, tm)
	m = fillForm(m, "local", "127.0.0.1", "5432", "mhmmdamir", "")
	tm, _ = m.saveConnForm()
	m = asModel(t, tm)
	if len(saved.Connections) != 1 || !saved.Connections[0].NoPassword {
		t.Fatalf("expected no_password: %#v", saved)
	}
	c := saved.Connections[0]
	if pw, src := m.passwordFor(c); pw != "" || src != "none" {
		t.Fatalf("empty add should authenticate: %q %q", pw, src)
	}
	if _, err := m.store.Get(c.ID()); err == nil {
		t.Fatal("empty password must not be stored in keychain")
	}
}

func TestEditRotatesKeyringID(t *testing.T) {
	m := testModel(t)
	m.saveCfg = func(config.File) error { return nil }
	m.conns[0].Source = config.SourceSaved
	old := m.conns[0]
	_ = m.store.Set(old.ID(), "oldpw")
	m.hasKey[old.ID()] = true
	m.memPW[old.ID()] = "oldpw"

	tm, _ := m.beginEdit()
	m = asModel(t, tm)
	if m.overlay != overlayEdit || m.formEditID != old.ID() {
		t.Fatal(m.overlay, m.formEditID)
	}
	m.formInputs[idxHost].SetValue("db.internal")
	m.formInputs[idxPass].SetValue("")
	tm, _ = m.saveConnForm()
	m = asModel(t, tm)
	newID := m.conns[m.connIdx].ID()
	if newID == old.ID() {
		t.Fatal("expected new id")
	}
	if _, err := m.store.Get(old.ID()); err == nil {
		t.Fatal("old keyring id should be gone")
	}
	got, err := m.store.Get(newID)
	if err != nil || got != "oldpw" {
		t.Fatalf("moved pw = %q %v", got, err)
	}
}

func TestEditReadsKeychainBeforeDelete(t *testing.T) {
	m := testModel(t)
	m.saveCfg = func(config.File) error { return nil }
	m.conns[0].Source = config.SourceSaved
	old := m.conns[0]
	_ = m.store.Set(old.ID(), "keypw")
	m.hasKey[old.ID()] = false
	delete(m.memPW, old.ID())

	tm, _ := m.beginEdit()
	m = asModel(t, tm)
	m.formInputs[idxHost].SetValue("db.internal")
	m.formInputs[idxPass].SetValue("")
	tm, _ = m.saveConnForm()
	m = asModel(t, tm)
	newID := m.conns[m.connIdx].ID()
	if newID == old.ID() {
		t.Fatal("expected new id")
	}
	if _, err := m.store.Get(old.ID()); err == nil {
		t.Fatal("old keyring id should be gone")
	}
	got, err := m.store.Get(newID)
	if err != nil || got != "keypw" {
		t.Fatalf("keychain password must move before Delete: %q %v", got, err)
	}
}

func TestDeleteRefusesDiscovered(t *testing.T) {
	m := testModel(t)
	m.conns[0].Source = ".env"
	m.focus = paneServers
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, ".env or compose") {
		t.Fatal(m.overlay, m.notice)
	}
}

func TestDeleteSavedConnection(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }
	m.conns[0].Source = config.SourceSaved
	id := m.conns[0].ID()
	_ = m.store.Set(id, "pw")
	m.hasKey[id] = true
	m.focus = paneServers
	tm, _ := m.beginDelete()
	m = asModel(t, tm)
	if m.overlay != overlayConfirmDelete {
		t.Fatal(m.overlay)
	}
	tm, _ = m.confirmDelete()
	m = asModel(t, tm)
	if len(m.conns) != 0 {
		t.Fatalf("conns = %#v", m.conns)
	}
	if len(saved.Connections) != 0 {
		t.Fatalf("cfg = %#v", saved)
	}
	if _, err := m.store.Get(id); err == nil {
		t.Fatal("keyring should be empty")
	}
}

func TestAddKeyFromAnywhere(t *testing.T) {
	m := testModel(t)
	m.focus = paneFiles
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if asModel(t, tm).overlay != overlayAdd {
		t.Fatal(asModel(t, tm).overlay)
	}
}

func TestFormTabAndEngineKeys(t *testing.T) {
	m := testModel(t)
	tm, _ := m.beginAdd()
	m = asModel(t, tm)
	tm, _ = m.handleFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = asModel(t, tm)
	if m.formEngine != config.EngineMySQL || m.formInputs[idxPort].Value() != "3306" {
		t.Fatal(m.formEngine, m.formInputs[idxPort].Value())
	}
	tm, _ = m.handleFormKey(tea.KeyMsg{Type: tea.KeyTab})
	m = asModel(t, tm)
	if m.formFocus != formName {
		t.Fatal(m.formFocus)
	}
	tm, _ = m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	if asModel(t, tm).overlay != overlayNone {
		t.Fatal("esc cancels")
	}
}

func TestHelpMentionsAddEditDelete(t *testing.T) {
	txt := helpText("dev")
	for _, s := range []string{"add server", "edit / save", "delete saved", "create a database"} {
		if !strings.Contains(txt, s) {
			t.Fatalf("missing %q in %s", s, txt)
		}
	}
	m := testModel(t)
	tm, _ := m.beginAdd()
	m = asModel(t, tm)
	out := m.viewOverlay()
	if !strings.Contains(out, "engine") || !strings.Contains(out, "keychain") {
		t.Fatal(out)
	}
	if strings.Contains(out, "database") {
		t.Fatal("add server form must not include a database field")
	}
}

func TestSaveConnectionNotices(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }

	tm, _ := m.beginAdd()
	m = asModel(t, tm)
	m = fillForm(m, "local", "127.0.0.1", "5432", "app", "s3cret")
	tm, _ = m.saveConnForm()
	m = asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, "saved") || !strings.Contains(m.notice, "connected") {
		t.Fatal(m.overlay, m.notice)
	}
	if len(saved.Connections) != 1 {
		t.Fatalf("%#v", saved)
	}
	if !m.dbReady || m.dbErr != "" {
		t.Fatalf("save should list dbs: ready=%v err=%q", m.dbReady, m.dbErr)
	}
	lines, _, _ := m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "connected") {
		t.Fatalf("save success must log: %#v", lines)
	}

	m = testModel(t)
	m.saveCfg = func(config.File) error { return nil }
	orig := testConn
	testConn = func(ctx context.Context, c config.Connection, password string, log db.LogFunc) error {
		return errString("password authentication failed")
	}
	t.Cleanup(func() { testConn = orig })
	tm, _ = m.beginAdd()
	m = asModel(t, tm)
	m = fillForm(m, "local", "127.0.0.1", "5432", "app", "bad")
	tm, _ = m.saveConnForm()
	m = asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, "saved") || !strings.Contains(m.notice, "connection failed") {
		t.Fatal(m.overlay, m.notice)
	}
	if m.dbReady || !strings.Contains(m.dbErr, "password authentication failed") {
		t.Fatalf("failed connect should show in databases pane: ready=%v err=%q", m.dbReady, m.dbErr)
	}
	lines, _, _ = m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "password authentication failed") {
		t.Fatalf("save fail must log: %#v", lines)
	}
}

func TestSaveListsDatabasesErrorShowsInPane(t *testing.T) {
	m := testModel(t)
	m.saveCfg = func(config.File) error { return nil }
	orig := listDBs
	listDBs = func(ctx context.Context, c config.Connection, password string, log db.LogFunc) ([]string, error) {
		return nil, errString("dial tcp: connection refused")
	}
	t.Cleanup(func() { listDBs = orig })
	tm, _ := m.beginAdd()
	m = asModel(t, tm)
	m = fillForm(m, "local", "127.0.0.1", "5432", "app", "s3cret")
	tm, _ = m.saveConnForm()
	m = asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, "list failed") {
		t.Fatal(m.overlay, m.notice)
	}
	if m.dbReady || !strings.Contains(m.dbErr, "connection refused") {
		t.Fatalf("list error should appear in middle pane: ready=%v err=%q", m.dbReady, m.dbErr)
	}
	if _, ok := m.currentDB(); ok {
		t.Fatal("must not pick a database after list failure")
	}
	lines, _, _ := m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "connection refused") {
		t.Fatalf("list error must log: %#v", lines)
	}
}
