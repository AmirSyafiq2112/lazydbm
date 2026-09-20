package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
	"github.com/AmirSyafiq2112/lazydbm/internal/db"
	"github.com/AmirSyafiq2112/lazydbm/internal/discover"
	"github.com/AmirSyafiq2112/lazydbm/internal/job"
	"github.com/AmirSyafiq2112/lazydbm/internal/secret"
	tea "github.com/charmbracelet/bubbletea"
)

func sampleConn() config.Connection {
	return config.Connection{
		Name: "epbt", Engine: config.EnginePostgres, Host: "127.0.0.1", Port: 5432, User: "app", Database: "epbt",
	}
}

func testModel(t *testing.T) model {
	t.Helper()
	m := newModel("/tmp/app", "test")
	m.store = secret.NewMemory()
	m.saveCfg = func(config.File) error { return nil }
	m.width, m.height, m.ready = 120, 40, true
	m.conns = []config.Connection{sampleConn()}
	m.files = []string{"dump.sql"}
	m.envPW = map[string]string{}
	m.memPW = map[string]string{}
	m.hasKey = map[string]bool{}
	return m
}

func asModel(t *testing.T, tm tea.Model) model {
	t.Helper()
	m, ok := tm.(model)
	if !ok {
		t.Fatalf("got %T", tm)
	}
	return m
}

func TestVisibleWindow(t *testing.T) {
	start, end := visibleWindow(10, 0, 5)
	if start != 0 || end != 5 {
		t.Fatalf("start=%d end=%d", start, end)
	}
	start, end = visibleWindow(10, 9, 5)
	if end != 10 || start != 5 {
		t.Fatalf("start=%d end=%d", start, end)
	}
	start, end = visibleWindow(3, 1, 0)
	if start != 0 || end != 0 {
		t.Fatal(start, end)
	}
}

func TestMoveIndex(t *testing.T) {
	if moveIndex(0, 0, "j") != 0 {
		t.Fatal("empty")
	}
	if moveIndex(0, 3, "j") != 1 {
		t.Fatal("j should increment")
	}
	if moveIndex(0, 3, "k") != 0 {
		t.Fatal("k at top stays")
	}
	if moveIndex(1, 3, "G") != 2 {
		t.Fatal("G should go last")
	}
	if moveIndex(2, 3, "g") != 0 {
		t.Fatal("g top")
	}
	if moveIndex(2, 3, "down") != 2 {
		t.Fatal("down at end")
	}
}

func TestIndexByID(t *testing.T) {
	conns := []config.Connection{sampleConn()}
	if indexByID(conns, conns[0].ID()) != 0 {
		t.Fatal("expected index 0")
	}
	if indexByID(conns, "nope") != -1 || indexByID(conns, "") != -1 {
		t.Fatal("expected -1")
	}
}

func TestExportAbsAndDefaultPath(t *testing.T) {
	got := exportAbs("/tmp", "out.sql")
	if got != filepath.Join("/tmp", "out.sql") {
		t.Fatalf("got %q", got)
	}
	if exportAbs("/tmp", "/abs/out.sql") != "/abs/out.sql" {
		t.Fatal("abs path should pass through")
	}
	if exportAbs("/tmp", "") != "" {
		t.Fatal("empty")
	}
	orig := now
	now = func() time.Time { return time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = orig })
	if defaultExportPath(sampleConn()) != "epbt-20260920-130000.sql" {
		t.Fatalf("path = %s", defaultExportPath(sampleConn()))
	}
}

func TestTruncateAndPad(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Fatal("no truncate")
	}
	got := truncate("hello world", 8)
	if got != "hello w…" {
		t.Fatalf("got %q", got)
	}
	if truncate("hi", 0) != "" || truncate("hi", 1) != "…" {
		t.Fatal(truncate("hi", 0), truncate("hi", 1))
	}
	if padRight("ab", 4) != "ab  " {
		t.Fatalf("%q", padRight("ab", 4))
	}
	if cutWidth("abcd", 2) != "ab" || cutWidthFrom("abcd", 2) != "cd" {
		t.Fatal(cutWidth("abcd", 2), cutWidthFrom("abcd", 2))
	}
}

func TestPasswordForSources(t *testing.T) {
	m := testModel(t)
	c := sampleConn()
	if _, src := m.passwordFor(c); src != "" {
		t.Fatal("none")
	}
	m.envPW[c.ID()] = "from-env"
	if pw, src := m.passwordFor(c); pw != "from-env" || src != "env" {
		t.Fatal(pw, src)
	}
	m.memPW[c.ID()] = "from-mem"
	if pw, src := m.passwordFor(c); pw != "from-mem" || src != "mem" {
		t.Fatal(pw, src)
	}
	m.memPW, m.envPW = map[string]string{}, map[string]string{}
	_ = m.store.Set(c.ID(), "from-key")
	m.hasKey[c.ID()] = true
	if pw, src := m.passwordFor(c); pw != "from-key" || src != "key" {
		t.Fatal(pw, src)
	}
}

func TestCurrentConnAndFile(t *testing.T) {
	m := testModel(t)
	if c, ok := m.currentConn(); !ok || c.Database != "epbt" {
		t.Fatal(c, ok)
	}
	if f, ok := m.currentFile(); !ok || f != "dump.sql" {
		t.Fatal(f, ok)
	}
	m.connIdx, m.fileIdx = -1, -1
	if _, ok := m.currentConn(); ok {
		t.Fatal("conn")
	}
	if _, ok := m.currentFile(); ok {
		t.Fatal("file")
	}
}

func TestPersistLastUsed(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }
	c := sampleConn()
	m = m.persistLastUsed(c)
	if saved.LastUsed != c.ID() || len(saved.Connections) != 1 {
		t.Fatalf("%#v", saved)
	}
}

func TestLoadDiscovered(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st := secret.NewMemory()
	msg := loadDiscovered(filepath.Join("..", "discover", "testdata", "mysql"), st)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(msg.res.Connections) != 1 {
		t.Fatalf("%#v", msg.res.Connections)
	}
}

func TestUpdateWindowAndDiscover(t *testing.T) {
	m := testModel(t)
	m.ready = false
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = asModel(t, tm)
	if !m.ready || m.width != 80 {
		t.Fatal(m.ready, m.width)
	}

	tm, _ = m.Update(discoveredMsg{err: errorsNew("boom")})
	m = asModel(t, tm)
	if m.overlay != overlayNotice || m.notice != "boom" {
		t.Fatal(m.overlay, m.notice)
	}

	c := sampleConn()
	tm, _ = m.Update(discoveredMsg{
		cfg:  config.File{LastUsed: c.ID()},
		res:  discover.Result{Connections: []config.Connection{c}, Files: []string{"a.sql"}, EnvPasswords: map[string]string{c.ID(): "e"}},
		keys: map[string]bool{c.ID(): true},
	})
	m = asModel(t, tm)
	if m.connIdx != 0 || m.files[0] != "a.sql" || m.envPW[c.ID()] != "e" {
		t.Fatalf("%#v", m)
	}
}

func errorsNew(s string) error { return errString(s) }

type errString string

func (e errString) Error() string { return string(e) }

func TestKeysNavigationAndOverlays(t *testing.T) {
	m := testModel(t)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = asModel(t, tm)
	if m.focus != paneFiles {
		t.Fatal(m.focus)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = asModel(t, tm)
	if !m.clearDB {
		t.Fatal("clear toggle")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = asModel(t, tm)
	if m.overlay != overlayHelp {
		t.Fatal(m.overlay)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, tm)
	if m.overlay != overlayNone {
		t.Fatal("help close")
	}

	m.envPW[sampleConn().ID()] = "pw"
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = asModel(t, tm)
	if m.overlay != overlayConfirmImport {
		t.Fatal(m.overlay)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = asModel(t, tm)
	if m.clearDB {
		t.Fatal("confirm overlay toggles clear")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, tm)
	if m.overlay != overlayNone {
		t.Fatal("cancel import")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = asModel(t, tm)
	if m.overlay != overlayExportPath {
		t.Fatal(m.overlay)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, tm)

	empty := testModel(t)
	empty.conns, empty.files = nil, nil
	tm, _ = empty.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if asModel(t, tm).overlay != overlayNotice {
		t.Fatal("import without conn")
	}
	tm, _ = empty.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if asModel(t, tm).overlay != overlayNotice {
		t.Fatal("password without conn")
	}
}

func TestPasswordSaveMemory(t *testing.T) {
	m := testModel(t)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = asModel(t, tm)
	if m.overlay != overlayPassword {
		t.Fatal(m.overlay)
	}
	m.pwInput.SetValue("hunter2")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.overlay != overlaySavePassword {
		t.Fatal(m.overlay)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = asModel(t, tm)
	c := sampleConn()
	if m.memPW[c.ID()] != "hunter2" || !m.hasKey[c.ID()] {
		t.Fatal(m.memPW, m.hasKey)
	}
	got, err := m.store.Get(c.ID())
	if err != nil || got != "hunter2" {
		t.Fatal(got, err)
	}
}

func TestImportExportJobsUseHooks(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	var imported, exported bool
	origI, origE := importDB, exportDB
	t.Cleanup(func() { importDB = origI; exportDB = origE })
	importDB = func(ctx context.Context, c config.Connection, password, file string, clear bool, log db.LogFunc) error {
		imported = true
		return nil
	}
	exportDB = func(ctx context.Context, c config.Connection, password, out string, log db.LogFunc) error {
		exported = true
		return nil
	}

	m, _ = m.startImportJob()
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if !imported {
		t.Fatal("import hook not called")
	}

	m, _ = m.startExportJob("out.sql")
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if !exported {
		t.Fatal("export hook not called")
	}
}

func TestBeginImportNeedsPassword(t *testing.T) {
	m := testModel(t)
	tm, _ := m.beginImport()
	if asModel(t, tm).overlay != overlayPassword {
		t.Fatal("should prompt password")
	}
}

func TestViewRendersPanesAndHelp(t *testing.T) {
	m := testModel(t)
	if newModel("/x", "v").View() != "loading lazydbm…" {
		t.Fatal("loading")
	}
	out := m.View()
	if !strings.Contains(out, "lazydbm") || !strings.Contains(out, "connections") || !strings.Contains(out, "dump.sql") {
		t.Fatalf("view = %s", out)
	}
	m.overlay = overlayHelp
	help := m.View()
	if !strings.Contains(help, "import") {
		t.Fatalf("help overlay missing: %s", help)
	}
	m.overlay = overlayConfirmImport
	m.clearDB = true
	if !strings.Contains(m.viewOverlay(), "DROP + CREATE") {
		t.Fatal(m.viewOverlay())
	}
}

func TestConnAndFileLines(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "x"
	lines := m.connLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "[env]") {
		t.Fatalf("%v", lines)
	}
	if m.fileLines()[0] != "dump.sql" {
		t.Fatal(m.fileLines())
	}
	m.files = nil
	if m.fileLines() != nil {
		t.Fatal("empty files")
	}
}

func TestPaneSizesHelpAndOverlayOn(t *testing.T) {
	l, mid, r, h := paneSizes(120, 30)
	if l <= 0 || mid <= 0 || r <= 0 || h <= 0 || l+mid+r != 120 {
		t.Fatal(l, mid, r, h)
	}
	txt := helpText("dev")
	if !strings.Contains(txt, "lazydbm dev") {
		t.Fatal(txt)
	}
	screen := "aaaaaaaaaa\nbbbbbbbbbb"
	out := overlayOn(screen, "XX", 10, 2)
	if !strings.Contains(out, "XX") {
		t.Fatalf("%q", out)
	}
}

func TestQuitCancelsRunningJob(t *testing.T) {
	m := testModel(t)
	started := make(chan struct{})
	block := make(chan struct{})
	m.runner.Start("x", func(ctx context.Context, log func(string)) error {
		close(started)
		<-block
		return ctx.Err()
	})
	<-started
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = asModel(t, tm)
	if cmd != nil {
		t.Fatal("first ctrl+c should cancel, not quit")
	}
	close(block)
	_, _ = job.Wait(m.runner, 2*time.Second)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("second ctrl+c should quit")
	}
}

func TestInitReturnsCmd(t *testing.T) {
	m := testModel(t)
	if m.Init() == nil || tick() == nil {
		t.Fatal("init/tick")
	}
}

func TestLogKeysAndTick(t *testing.T) {
	m := testModel(t)
	m.logVP.SetContent(strings.Repeat("line\n", 40))
	m.handleLogKeys("j")
	if m.stickLog && m.logVP.AtBottom() {
		// may restick if already at bottom
	}
	m.stickLog = true
	m.handleLogKeys("G")
	if !m.stickLog {
		t.Fatal("G should stick")
	}
	m.handleLogKeys("g")
	m.handleLogKeys("k")
	m.handleLogKeys("pgdown")
	m.handleLogKeys("pgup")
	m.syncLogs()
	tm, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("tick should reschedule")
	}
	_ = asModel(t, tm)
}

func TestMoreOverlaysAndKeys(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = asModel(t, tm)
	if m.focus != paneLogs {
		t.Fatalf("shift-tab focus = %d", m.focus)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = asModel(t, tm)
	if m.focus != paneFiles {
		t.Fatal(m.focus)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = asModel(t, tm)

	m.focus = paneFiles
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.overlay != overlayConfirmImport {
		t.Fatal(m.overlay)
	}
	m.overlay = overlayNone

	m.focus = paneConns
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.overlay != overlayNone {
		t.Fatal("password already present")
	}

	m.overlay = overlayPassword
	if !strings.Contains(m.viewOverlay(), "password") {
		t.Fatal(m.viewOverlay())
	}
	m.overlay = overlaySavePassword
	if !strings.Contains(m.viewOverlay(), "keychain") {
		t.Fatal(m.viewOverlay())
	}
	m.overlay = overlayExportPath
	if !strings.Contains(m.viewOverlay(), "export") {
		t.Fatal(m.viewOverlay())
	}
	m.overlay = overlayNotice
	m.notice = "hi"
	if !strings.Contains(m.viewOverlay(), "hi") {
		t.Fatal(m.viewOverlay())
	}

	tm, _ = m.savePassword(false)
	m = asModel(t, tm)
	if m.overlay != overlayNone {
		t.Fatal("session-only save")
	}

	fail := failSet{}
	m.store = fail
	m.pwInput.SetValue("x")
	tm, _ = m.savePassword(true)
	if asModel(t, tm).overlay != overlayNotice {
		t.Fatal("keychain failure should notice")
	}
}

type failSet struct{}

func (failSet) Get(string) (string, error) { return "", secret.ErrNotFound }
func (failSet) Set(string, string) error   { return errString("nope") }
func (failSet) Delete(string) error        { return nil }

func TestJobAlreadyRunningNotice(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	started := make(chan struct{})
	block := make(chan struct{})
	m.runner.Start("busy", func(ctx context.Context, log func(string)) error {
		close(started)
		<-block
		return nil
	})
	<-started
	tm, _ := m.beginImport()
	if asModel(t, tm).overlay != overlayNotice {
		t.Fatal("expected busy notice")
	}
	tm, _ = m.beginExport()
	if asModel(t, tm).overlay != overlayNotice {
		t.Fatal("expected busy notice")
	}
	close(block)
	_, _ = job.Wait(m.runner, 2*time.Second)
}

func TestQQuits(t *testing.T) {
	m := testModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should quit")
	}
}
