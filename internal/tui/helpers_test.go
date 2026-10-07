package tui

import (
	"context"
	"fmt"
	"os"
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
	"github.com/charmbracelet/lipgloss"
)

func sampleConn() config.Connection {
	return config.Connection{
		Name: "epbt", Engine: config.EnginePostgres, Host: "127.0.0.1", Port: 5432, User: "app", Database: "epbt",
	}
}

func testModel(t *testing.T) model {
	t.Helper()
	prevMissing, prevTest, prevList, prevSchemas, prevEnsure, prevPreview, prevDump := missingTools, testConn, listDBs, listSchemas, ensureDB, previewSchemas, dumpSchemas
	prevListTables, prevPreviewTables, prevDumpTables := listTables, previewTables, dumpTables
	missingTools = func(config.Connection, string, bool, bool) []string { return nil }
	testConn = func(context.Context, config.Connection, string, db.LogFunc) error { return nil }
	listDBs = func(context.Context, config.Connection, string, db.LogFunc) ([]string, error) {
		return []string{"epbt", "postgres"}, nil
	}
	listSchemas = func(context.Context, config.Connection, string, db.LogFunc) ([]string, error) {
		return []string{"public", "app"}, nil
	}
	ensureDB = func(context.Context, config.Connection, string, db.LogFunc) error { return nil }
	previewSchemas = func(_ context.Context, _ config.Connection, _, schema string, _ db.LogFunc) ([]db.SchemaStat, error) {
		return []db.SchemaStat{{Name: schema, Bytes: 4096}}, nil
	}
	dumpSchemas = func(string) ([]string, error) { return nil, nil }
	listTables = func(context.Context, config.Connection, string, string, db.LogFunc) ([]string, error) {
		return []string{"orders", "items"}, nil
	}
	previewTables = func(_ context.Context, _ config.Connection, _, schema, table string, _ db.LogFunc) ([]db.TableStat, error) {
		return []db.TableStat{{Schema: schema, Name: table, Bytes: 4096}}, nil
	}
	dumpTables = func(string) ([]db.TableRef, error) { return nil, nil }
	t.Cleanup(func() {
		missingTools, testConn, listDBs, listSchemas, ensureDB, previewSchemas, dumpSchemas = prevMissing, prevTest, prevList, prevSchemas, prevEnsure, prevPreview, prevDump
		listTables, previewTables, dumpTables = prevListTables, prevPreviewTables, prevDumpTables
	})
	m := newModel("/tmp/app", "test")
	m.store = secret.NewMemory()
	m.saveCfg = func(config.File) error { return nil }
	m.width, m.height, m.ready = 120, 40, true
	m.conns = []config.Connection{sampleConn()}
	m.files = []string{"dump.sql"}
	m.databases = []string{"postgres", "epbt"}
	m.dbIdx = 1
	m.dbReady = true
	m.dbLists = map[string][]string{sampleConn().ID(): {"postgres", "epbt"}}
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
	if defaultExportPath("epbt") != "epbt-20260920-130000.sql" {
		t.Fatalf("path = %s", defaultExportPath("epbt"))
	}
}

func TestSafeExportBasename(t *testing.T) {
	if _, err := safeExportBasename(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := safeExportBasename("."); err == nil {
		t.Fatal("dot")
	}
	if _, err := safeExportBasename(".."); err == nil {
		t.Fatal("dotdot")
	}
	if _, err := safeExportBasename("foo..bar.sql"); err == nil {
		t.Fatal(".. in name")
	}
	got, err := safeExportBasename("/tmp/out.sql")
	if err != nil || got != "out.sql" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestResolveExportPath(t *testing.T) {
	cwd := "/tmp/app"
	got, err := resolveExportPath(cwd, "out.sql")
	if err != nil {
		t.Fatal(err)
	}
	root, err := absDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "out.sql") {
		t.Fatalf("got %q want %q", got, filepath.Join(root, "out.sql"))
	}
	if _, err := resolveExportPath(cwd, "../secret.sql"); err == nil {
		t.Fatal("relative traversal must be rejected")
	}
	abs := "/abs/out.sql"
	got, err = resolveExportPath(cwd, abs)
	if err != nil || got != abs {
		t.Fatalf("absolute path should pass: %q %v", got, err)
	}
}

func TestResolveExportPathTempCwd(t *testing.T) {
	cwd := t.TempDir()
	orig := now
	now = func() time.Time { return time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = orig })
	name := defaultExportPath("epbt")
	got, err := resolveExportPath(cwd, name)
	if err != nil {
		t.Fatalf("default export in temp cwd rejected: %v (cwd=%q)", err, cwd)
	}
	root, err := absDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, name)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestThemeUsesTerminalPalette(t *testing.T) {
	if colGreen != lipgloss.Color("2") || colCyan != lipgloss.Color("6") || colRed != lipgloss.Color("1") {
		t.Fatal("theme colors must be ANSI 0-15, not hex")
	}
	if _, ok := paneBorder(true).GetBackground().(lipgloss.NoColor); !ok {
		t.Fatal("focused pane must not paint a custom background")
	}
	if _, ok := paneBorder(false).GetBackground().(lipgloss.NoColor); !ok {
		t.Fatal("inactive pane must not paint a custom background")
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
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("abcd")
	gotCut := cutWidth(red, 2)
	if lipgloss.Width(gotCut) != 2 {
		t.Fatalf("ANSI cutWidth width = %d (%q)", lipgloss.Width(gotCut), gotCut)
	}
	rest := cutWidthFrom(red, 2)
	if lipgloss.Width(rest) != 2 {
		t.Fatalf("ANSI cutWidthFrom width = %d (%q)", lipgloss.Width(rest), rest)
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

func TestPasswordForEmptyIsAuth(t *testing.T) {
	m := testModel(t)
	c := sampleConn()
	m.memPW[c.ID()] = ""
	if pw, src := m.passwordFor(c); pw != "" || src != "none" {
		t.Fatalf("empty mem password should count as auth, got %q %q", pw, src)
	}
	m.memPW = map[string]string{}
	m.conns[0].NoPassword = true
	if pw, src := m.passwordFor(c); pw != "" || src != "none" {
		t.Fatalf("no_password flag should count as auth, got %q %q", pw, src)
	}
	m.conns[0].NoPassword = false
	m.envPW[c.ID()] = ""
	if pw, src := m.passwordFor(c); pw != "" || src != "none" {
		t.Fatalf("empty env password should count as auth, got %q %q", pw, src)
	}
}

func TestEmptyPasswordConnectsWithoutPrompt(t *testing.T) {
	m := testModel(t)
	m.memPW[sampleConn().ID()] = ""
	tm, _ := m.connectServer()
	m = asModel(t, tm)
	if m.overlay == overlayPassword {
		t.Fatal("empty password must not prompt")
	}
	if !m.dbReady {
		t.Fatal("should list databases")
	}
}

func TestSaveEmptyPasswordPersistsFlag(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }
	m.conns[0].Source = config.SourceSaved
	m.pwInput.SetValue("")
	id := m.conns[0].ID()
	_ = m.store.Set(id, "old")
	m.hasKey[id] = true
	tm, _ := m.savePassword(true)
	m = asModel(t, tm)
	if !m.conns[0].NoPassword {
		t.Fatal("expected no_password on connection")
	}
	if _, src := m.passwordFor(m.conns[0]); src != "none" {
		t.Fatal("empty persist should authenticate")
	}
	if _, err := m.store.Get(id); err == nil {
		t.Fatal("empty password must not stay in keychain")
	}
	if len(saved.Connections) != 1 || !saved.Connections[0].NoPassword {
		t.Fatalf("config = %#v", saved)
	}
}

func TestCurrentConnAndFile(t *testing.T) {
	m := testModel(t)
	if c, ok := m.currentConn(); !ok || c.User != "app" {
		t.Fatal(c, ok)
	}
	if db, ok := m.currentDB(); !ok || db != "epbt" {
		t.Fatal(db, ok)
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
	c.Source = config.SourceSaved
	m = m.persistLastUsed(c)
	if saved.LastUsed != c.ID() || len(saved.Connections) != 1 {
		t.Fatalf("%#v", saved)
	}
}

func TestPersistLastUsedSkipsDiscovered(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }
	c := sampleConn()
	c.Source = ".env"
	m = m.persistLastUsed(c)
	if saved.LastUsed != c.ID() {
		t.Fatalf("LastUsed = %q", saved.LastUsed)
	}
	if len(saved.Connections) != 0 {
		t.Fatalf(".env connection must not be upserted: %#v", saved.Connections)
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
	if m.focus != paneDBs {
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
	empty.conns, empty.files, empty.databases, empty.dbReady = nil, nil, nil, false
	tm, _ = empty.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if asModel(t, tm).overlay != overlayNotice {
		t.Fatal("import without server")
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

	exported = false
	m, _ = m.startExportJob("../secret.sql")
	if m.overlay != overlayNotice {
		t.Fatal("relative traversal must be rejected")
	}
	lines, _, _ := m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "current directory") {
		t.Fatalf("blocked export must log: %#v", lines)
	}
	if exported {
		t.Fatal("export must not start for traversal path")
	}

	var outPath string
	exportDB = func(ctx context.Context, c config.Connection, password, out string, log db.LogFunc) error {
		outPath = out
		return nil
	}
	m.overlay = overlayNone
	m, _ = m.startExportJob("/abs/out.sql")
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if outPath != "/abs/out.sql" {
		t.Fatalf("absolute export path = %q", outPath)
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
	if !strings.Contains(out, "lazydbm") || !strings.Contains(out, "servers") || !strings.Contains(out, "databases") || !strings.Contains(out, "dump.sql") {
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
	l, mid, r, topH, logH := paneSizes(120, 28)
	if l <= 0 || mid <= 0 || r <= 0 || topH <= 0 || logH <= 0 || l+mid+r != 120 || topH+logH != 28 {
		t.Fatal(l, mid, r, topH, logH)
	}
	l, mid, r, topH, logH = paneSizes(40, 20)
	if l+mid+r != 40 || topH+logH != 20 {
		t.Fatalf("narrow panes %d+%d+%d top=%d log=%d", l, mid, r, topH, logH)
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
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(strings.Repeat("x", 40))
	out = overlayOn(styled+"\n"+styled, "HELLO", 40, 2)
	if !strings.Contains(out, "HELLO") {
		t.Fatalf("ANSI overlay missing HELLO: %q", out)
	}
	ellipsis := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "…" || strings.TrimSpace(line) == "..." {
			ellipsis++
		}
		if lipgloss.Width(line) > 40 {
			t.Fatalf("overlay line too wide: %d", lipgloss.Width(line))
		}
	}
	if ellipsis > 0 {
		t.Fatalf("overlay collapsed to ellipsis: %q", out)
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

	m.focus = paneServers
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.overlay != overlayNone {
		t.Fatal("password already present")
	}
	if !m.dbReady || len(m.databases) != 2 {
		t.Fatalf("enter on server should list databases: ready=%v %#v", m.dbReady, m.databases)
	}

	m.overlay = overlayPassword
	if !strings.Contains(m.viewOverlay(), "password") {
		t.Fatal(m.viewOverlay())
	}
	m.pwInput.SetValue("secret")
	m.overlay = overlaySavePassword
	if !strings.Contains(m.viewOverlay(), "keychain") {
		t.Fatal(m.viewOverlay())
	}
	m.pwInput.SetValue("")
	m.overlay = overlaySavePassword
	if !strings.Contains(m.viewOverlay(), "no-password") {
		t.Fatal(m.viewOverlay())
	}
	m.overlay = overlayCreateDB
	if !strings.Contains(m.viewOverlay(), "create") {
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

func TestBlockedImportExportLogs(t *testing.T) {
	empty := testModel(t)
	empty.conns, empty.files, empty.databases, empty.dbReady = nil, nil, nil, false
	tm, _ := empty.startImportJob()
	m := asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, "select a server") {
		t.Fatal(m.overlay, m.notice)
	}
	lines, _, _ := m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "select a server") {
		t.Fatalf("blocked import must log: %#v", lines)
	}

	m = testModel(t)
	m.dbReady = false
	m.databases = nil
	tm, _ = m.startImportJob()
	m = asModel(t, tm)
	if !strings.Contains(m.notice, "database") {
		t.Fatal(m.notice)
	}
	lines, _, _ = m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "database") {
		t.Fatalf("blocked import must log missing database: %#v", lines)
	}

	m = testModel(t)
	m.files = nil
	tm, _ = m.startImportJob()
	m = asModel(t, tm)
	if !strings.Contains(m.notice, "dump file") {
		t.Fatal(m.notice)
	}
	lines, _, _ = m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "dump file") {
		t.Fatalf("blocked import must log missing file: %#v", lines)
	}

	m = testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	orig := missingTools
	missingTools = func(config.Connection, string, bool, bool) []string { return []string{"psql"} }
	t.Cleanup(func() { missingTools = orig })
	tm, _ = m.startImportJob()
	m = asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, "psql") {
		t.Fatal(m.overlay, m.notice)
	}
	lines, _, _ = m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "missing client tools") {
		t.Fatalf("missing tools must log: %#v", lines)
	}
}

func TestConfirmImportStartsJob(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	var imported bool
	origI := importDB
	t.Cleanup(func() { importDB = origI })
	importDB = func(ctx context.Context, c config.Connection, password, file string, clear bool, log db.LogFunc) error {
		imported = true
		log("$ psql -h 127.0.0.1 -d epbt")
		return nil
	}
	tm, _ := m.beginImport()
	m = asModel(t, tm)
	if m.overlay != overlayConfirmImport {
		t.Fatal(m.overlay)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if !imported {
		t.Fatal("confirm enter must start import")
	}
	m.syncLogs()
	out := m.View()
	if !strings.Contains(out, "import dump.sql") && !strings.Contains(out, "SUCCESS") {
		lines, _, _ := m.runner.Snapshot()
		t.Fatalf("log pane missing job output: view=%q lines=%#v", out, lines)
	}
}

func TestPasswordContinuesImport(t *testing.T) {
	m := testModel(t)
	tm, _ := m.beginImport()
	m = asModel(t, tm)
	if m.overlay != overlayPassword || m.pending != pendingImport {
		t.Fatal(m.overlay, m.pending)
	}
	m.pwInput.SetValue("hunter2")
	tm, _ = m.savePassword(false)
	m = asModel(t, tm)
	if m.overlay != overlayConfirmImport {
		t.Fatalf("after password, expected confirm import, got %d", m.overlay)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 80, 24
	m.sizeLogPane()
	m.overlay = overlayPassword
	out := m.View()
	ellipsis := 0
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line %d width %d > %d", i, w, m.width)
		}
		trim := strings.TrimSpace(line)
		if trim == "…" || trim == "..." {
			ellipsis++
		}
	}
	if ellipsis > 2 {
		t.Fatalf("layout collapsed to ellipsis (%d lines):\n%s", ellipsis, out)
	}
	if !strings.Contains(out, "servers") || !strings.Contains(out, "databases") {
		t.Fatalf("missing panes:\n%s", out)
	}
	if strings.Contains(m.pwInput.View(), "password") {
		t.Fatalf("password placeholder looks like a value: %q", m.pwInput.View())
	}
}

func TestTickSyncsLogs(t *testing.T) {
	m := testModel(t)
	m.runner.Append("hello from job")
	tm, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("tick should reschedule")
	}
	m = asModel(t, tm)
	if !strings.Contains(m.logVP.View(), "hello from job") && !strings.Contains(m.View(), "hello from job") {
		t.Fatalf("tick did not sync logs: vp=%q view=%q", m.logVP.View(), m.View())
	}
}

func TestConnectServerListsAndPrefersSuggestedDB(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	m.dbReady = false
	m.databases = nil
	m.dbIdx = 0
	m.dbLists = map[string][]string{}
	tm, _ := m.connectServer()
	m = asModel(t, tm)
	if m.overlay != overlayNone {
		t.Fatal(m.overlay, m.notice)
	}
	if !m.dbReady || len(m.databases) != 2 {
		t.Fatalf("listed = %#v", m.databases)
	}
	db, ok := m.currentDB()
	if !ok || db != "epbt" {
		t.Fatalf("suggested db should be selected: %q %v", db, ok)
	}
	lines, _, _ := m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "listed 2 databases") {
		t.Fatalf("connect must log: %#v", lines)
	}
}

func TestConnectFailureShowsInMiddlePane(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	orig := testConn
	testConn = func(ctx context.Context, c config.Connection, password string, log db.LogFunc) error {
		return errString("connection refused")
	}
	t.Cleanup(func() { testConn = orig })
	tm, _ := m.connectServer()
	m = asModel(t, tm)
	if m.overlay != overlayNotice || !strings.Contains(m.notice, "connection refused") {
		t.Fatal(m.overlay, m.notice)
	}
	if m.dbReady || !strings.Contains(m.dbErr, "connection refused") {
		t.Fatalf("middle pane err=%q ready=%v", m.dbErr, m.dbReady)
	}
	if _, ok := m.currentDB(); ok {
		t.Fatal("must not pick a db after list/connect failure")
	}
	out := m.View()
	if !strings.Contains(out, "connection refused") {
		t.Fatalf("databases pane should show error:\n%s", out)
	}
}

func TestImportUsesSelectedDatabase(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	m.dbIdx = 0
	var gotDB string
	origI := importDB
	t.Cleanup(func() { importDB = origI })
	importDB = func(ctx context.Context, c config.Connection, password, file string, clear bool, log db.LogFunc) error {
		gotDB = c.Database
		return nil
	}
	m, _ = m.startImportJob()
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if gotDB != "postgres" {
		t.Fatalf("import used %q, want selected postgres", gotDB)
	}
}

func TestPersistLastUsedStoresLastDatabase(t *testing.T) {
	m := testModel(t)
	var saved config.File
	m.saveCfg = func(f config.File) error { saved = f; return nil }
	c := sampleConn()
	c.Source = config.SourceSaved
	m.conns[0] = c
	m.dbIdx = 1
	m = m.persistLastUsed(c)
	if saved.LastUsed != c.ID() {
		t.Fatalf("LastUsed = %q", saved.LastUsed)
	}
	if len(saved.Connections) != 1 || saved.Connections[0].LastDatabase != "epbt" {
		t.Fatalf("%#v", saved.Connections)
	}
}

func TestJKMovesDatabasesAndServers(t *testing.T) {
	m := testModel(t)
	m.focus = paneDBs
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = asModel(t, tm)
	if db, _ := m.currentDB(); db != "postgres" {
		t.Fatal(db)
	}
	m.focus = paneServers
	m.conns = append(m.conns, config.Connection{Engine: config.EngineMySQL, Host: "127.0.0.1", Port: 3306, User: "root"})
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = asModel(t, tm)
	if m.connIdx != 1 {
		t.Fatal(m.connIdx)
	}
	if m.dbReady {
		t.Fatal("switching servers without cache should clear dbs")
	}
}

func TestCreateDatabaseOverlay(t *testing.T) {
	m := testModel(t)
	m.memPW[sampleConn().ID()] = ""
	var ensured string
	ensureDB = func(ctx context.Context, c config.Connection, password string, log db.LogFunc) error {
		ensured = c.Database
		return nil
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = asModel(t, tm)
	if m.overlay != overlayCreateDB {
		t.Fatalf("overlay = %d", m.overlay)
	}
	m.dbInput.SetValue("epbt30_dev")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if ensured != "epbt30_dev" {
		t.Fatalf("ensured = %q", ensured)
	}
	if db, _ := m.currentDB(); db != "epbt30_dev" {
		t.Fatalf("selected = %q databases=%#v", db, m.databases)
	}
	if m.focus != paneDBs {
		t.Fatal(m.focus)
	}
}

func TestClearSchemaDoesNotDropDatabase(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	m.schemaMode = true
	m.schemaDB = "epbt"
	m.schemas = []string{"public", "app"}
	m.schemaIdx = 1
	m.clearDB = true

	tm, _ := m.beginImport()
	m = asModel(t, tm)
	overlay := m.viewOverlay()
	if !strings.Contains(overlay, "epbt.app") || !strings.Contains(overlay, "DROP SCHEMA app") {
		t.Fatalf("overlay = %s", overlay)
	}
	if strings.Contains(overlay, "DROP + CREATE") || strings.Contains(overlay, "all data in") {
		t.Fatalf("schema clear must not offer a database drop: %s", overlay)
	}

	var cleared string
	var importedClear bool
	origI, origR := importDB, resetSchema
	t.Cleanup(func() { importDB, resetSchema = origI, origR })
	resetSchema = func(_ context.Context, c config.Connection, _, schema string, _ db.LogFunc) error {
		cleared = c.Database + "." + schema
		return nil
	}
	importDB = func(_ context.Context, _ config.Connection, _, _ string, clear bool, _ db.LogFunc) error {
		importedClear = clear
		return nil
	}
	m, _ = m.startImportJob()
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if cleared != "epbt.app" || importedClear {
		t.Fatalf("cleared=%q import clear=%v", cleared, importedClear)
	}
}

func TestClearSchemasFromDump(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	m.schemaMode = true
	m.schemaDB = "epbt"
	m.schemas = []string{"mswn", "spss"}
	m.schemaIdx = 0
	m.clearDB = true
	m.files = []string{"epbt-mswn.sql"}
	dumpSchemas = func(string) ([]string, error) {
		return []string{"mswn", "spss", "spbt"}, nil
	}

	tm, _ := m.beginImport()
	m = asModel(t, tm)
	overlay := m.viewOverlay()
	if !strings.Contains(overlay, "DROP 3 SCHEMAS") || !strings.Contains(overlay, "mswn") || !strings.Contains(overlay, "spss") || !strings.Contains(overlay, "spbt") {
		t.Fatalf("overlay = %s", overlay)
	}
	if strings.Contains(overlay, "DROP + CREATE") {
		t.Fatalf("schema clear must not offer a database drop: %s", overlay)
	}

	var cleared []string
	var importedClear bool
	origI, origR := importDB, resetSchema
	t.Cleanup(func() { importDB, resetSchema = origI, origR })
	resetSchema = func(_ context.Context, c config.Connection, _, schema string, _ db.LogFunc) error {
		cleared = append(cleared, c.Database+"."+schema)
		return nil
	}
	importDB = func(_ context.Context, _ config.Connection, _, _ string, clear bool, _ db.LogFunc) error {
		importedClear = clear
		return nil
	}
	m, _ = m.startImportJob()
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cleared, ",") != "epbt.mswn,epbt.spss,epbt.spbt" || importedClear {
		t.Fatalf("cleared=%v import clear=%v", cleared, importedClear)
	}
}

func TestTableListExportAndClear(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	m.focus = paneDBs

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if !m.tableMode || m.tableSchema != "public" || m.tableDB != "epbt" {
		t.Fatalf("tables mode=%v schema=%q db=%q", m.tableMode, m.tableSchema, m.tableDB)
	}
	if !strings.Contains(m.View(), "tables · epbt.public") || !strings.Contains(m.View(), "orders") {
		t.Fatal(m.View())
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = asModel(t, tm)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = asModel(t, tm)
	if table, _ := m.currentTable(); table != "items" {
		t.Fatalf("filtered table = %q", table)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = asModel(t, tm)
	if m.overlay != overlayExportPreview || !strings.Contains(m.viewOverlay(), "public.items") || !strings.Contains(m.viewOverlay(), "4.0 KB") {
		t.Fatal(m.viewOverlay())
	}
	var got []string
	orig := exportTableSet
	t.Cleanup(func() { exportTableSet = orig })
	exportTableSet = func(_ context.Context, c config.Connection, _ string, tables []db.TableStat, _ string, _ db.LogFunc) error {
		for _, table := range tables {
			got = append(got, c.Database+"."+table.Label())
		}
		return nil
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "epbt.public.items" {
		t.Fatalf("exported %v", got)
	}

	m.clearDB = true
	dumpTables = func(string) ([]db.TableRef, error) {
		return []db.TableRef{{Schema: "public", Name: "items"}, {Schema: "public", Name: "orders"}}, nil
	}
	tm, _ = m.beginImport()
	m = asModel(t, tm)
	overlay := m.viewOverlay()
	if !strings.Contains(overlay, "DROP 2 TABLES") || !strings.Contains(overlay, "public.items") || !strings.Contains(overlay, "public.orders") {
		t.Fatalf("overlay = %s", overlay)
	}
	var dropped string
	origR := resetTables
	t.Cleanup(func() { resetTables = origR })
	resetTables = func(_ context.Context, _ config.Connection, _ string, tables []db.TableRef, _ db.LogFunc) error {
		for _, table := range tables {
			if dropped != "" {
				dropped += ","
			}
			dropped += table.Label()
		}
		return nil
	}
	origI := importDB
	t.Cleanup(func() { importDB = origI })
	importDB = func(context.Context, config.Connection, string, string, bool, db.LogFunc) error { return nil }
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if dropped != "public.items,public.orders" {
		t.Fatalf("dropped %q", dropped)
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = asModel(t, tm)
	if m.tableMode || !m.schemaMode {
		t.Fatalf("backspace tableMode=%v schemaMode=%v", m.tableMode, m.schemaMode)
	}
}

func TestProtectedImport(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	m.conns[0].Source = config.SourceSaved
	m.focus = paneDBs

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	m = asModel(t, tm)
	if !m.conns[0].Protects("epbt") || !strings.Contains(m.View(), "epbt  protected") {
		t.Fatalf("protected = %#v\n%s", m.conns[0].Protected, m.View())
	}

	var imported bool
	origI := importDB
	t.Cleanup(func() { importDB = origI })
	importDB = func(context.Context, config.Connection, string, string, bool, db.LogFunc) error {
		imported = true
		return nil
	}
	tm, _ = m.beginImport()
	m = asModel(t, tm)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.overlay != overlayProtectedImport || imported {
		t.Fatalf("overlay=%d imported=%v", m.overlay, imported)
	}
	if !strings.Contains(m.viewOverlay(), "protected database") || !strings.Contains(m.viewOverlay(), "epbt") {
		t.Fatal(m.viewOverlay())
	}
	m.dbInput.SetValue("local")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.overlay != overlayProtectedImport || imported || m.guardErr == "" {
		t.Fatalf("wrong name should stay, overlay=%d err=%q", m.overlay, m.guardErr)
	}
	m.dbInput.SetValue("epbt")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if !imported {
		t.Fatal("matching name should import")
	}
}

func TestRefreshSQLFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "new.sql"), []byte("-- new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.cwd = dir
	m.files = []string{"old.sql"}
	m.fileIdx = 0

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = asModel(t, tm)
	if cmd == nil {
		t.Fatal("refresh should rescan")
	}
	if len(m.files) != 1 || m.files[0] != "new.sql" {
		t.Fatalf("files = %#v", m.files)
	}
	if !strings.Contains(m.View(), "new.sql") {
		t.Fatal("file pane did not show new.sql")
	}
	lines, _, _ := m.runner.Snapshot()
	if !strings.Contains(strings.Join(lines, "\n"), "refreshed 1 sql files") {
		t.Fatalf("log = %#v", lines)
	}
}

func TestSchemaExport(t *testing.T) {
	m := testModel(t)
	m.focus = paneDBs
	m.envPW[sampleConn().ID()] = "pw"

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if !m.schemaMode || m.schemaDB != "epbt" {
		t.Fatalf("schema mode = %v db=%q", m.schemaMode, m.schemaDB)
	}
	if len(m.schemas) != 2 || m.schemas[0] != "public" {
		t.Fatalf("schemas = %#v", m.schemas)
	}
	view := m.View()
	if !strings.Contains(view, "schemas") || !strings.Contains(view, "public") || !strings.Contains(view, "app") {
		t.Fatalf("schema pane missing: %s", view)
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = asModel(t, tm)
	if schema, _ := m.currentSchema(); schema != "app" {
		t.Fatalf("schema = %q", schema)
	}
	previewSchemas = func(_ context.Context, _ config.Connection, _, schema string, _ db.LogFunc) ([]db.SchemaStat, error) {
		return []db.SchemaStat{{Name: schema, Bytes: 4096}, {Name: "auth", Bytes: 1048576}}, nil
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = asModel(t, tm)
	if m.overlay != overlayExportPreview || m.exportSchema != "app" {
		t.Fatalf("overlay=%d schema=%q", m.overlay, m.exportSchema)
	}
	preview := m.viewOverlay()
	if !strings.Contains(preview, "epbt.app") || !strings.Contains(preview, "auth") || !strings.Contains(preview, "1.0 MB") || !strings.Contains(preview, "4.0 KB") {
		t.Fatal(preview)
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, tm)
	if m.overlay != overlayNone || m.runner.Running() {
		t.Fatal("esc should cancel before export")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = asModel(t, tm)
	var gotSchemas []string
	var gotDB string
	orig := exportSchemas
	t.Cleanup(func() { exportSchemas = orig })
	exportSchemas = func(_ context.Context, c config.Connection, _ string, schemas []string, _ string, _ db.LogFunc) error {
		gotSchemas = append([]string(nil), schemas...)
		gotDB = c.Database
		return nil
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if _, err := job.Wait(m.runner, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if gotDB != "epbt" || strings.Join(gotSchemas, ",") != "app,auth" {
		t.Fatalf("exported %s %v", gotDB, gotSchemas)
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = asModel(t, tm)
	if m.schemaMode || m.exportSchema != "" {
		t.Fatal("backspace should return to databases")
	}
	if !strings.Contains(m.View(), "databases") {
		t.Fatal(m.View())
	}
}

func TestLogFullViewScrolls(t *testing.T) {
	m := testModel(t)
	m.runner.Append("hello from job")
	for i := 0; i < 80; i++ {
		m.runner.Append(fmt.Sprintf("line %d", i))
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = asModel(t, tm)
	if !m.logFull {
		t.Fatal("f should open the full log")
	}
	out := m.View()
	if !strings.Contains(out, "full") || !strings.Contains(out, "esc close") {
		t.Fatalf("full log view = %q", out)
	}
	if strings.Contains(out, "servers") {
		t.Fatal("full log should hide the other panes")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = asModel(t, tm)
	if m.logVP.YOffset != 0 {
		t.Fatalf("g offset = %d", m.logVP.YOffset)
	}
	top := m.View()
	if !strings.Contains(top, "hello from job") {
		t.Fatalf("top of log = %q", top)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = asModel(t, tm)
	if m.logVP.YOffset < 1 {
		t.Fatalf("j offset = %d", m.logVP.YOffset)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, tm)
	if m.logFull {
		t.Fatal("esc should close the full log")
	}
	if !strings.Contains(m.View(), "servers") {
		t.Fatal("panes should return")
	}
}

func TestFileSearchFiltersSelection(t *testing.T) {
	m := testModel(t)
	m.focus = paneFiles
	m.files = []string{"dump.sql", "reports/other.sql", "seed/users.sql"}
	m.fileIdx = 0

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = asModel(t, tm)
	if !m.fileSearch {
		t.Fatal("slash should start file search")
	}
	for _, r := range []rune{'o', 't', 'h', 'e', 'r'} {
		tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = asModel(t, tm)
	}
	if got := m.fileLines(); len(got) != 1 || got[0] != "reports/other.sql" {
		t.Fatalf("filtered = %#v", got)
	}
	if f, ok := m.currentFile(); !ok || f != "reports/other.sql" {
		t.Fatalf("selected = %q ok=%v", f, ok)
	}
	if !strings.Contains(m.View(), "files /other") {
		t.Fatal(m.View())
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.fileSearch || m.fileQuery != "other" {
		t.Fatalf("enter should keep filter: search=%v query=%q", m.fileSearch, m.fileQuery)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = asModel(t, tm)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, tm)
	if m.fileSearch || m.fileQuery != "" || len(m.fileLines()) != 3 {
		t.Fatalf("esc should clear search: %+v lines=%#v", m.fileQuery, m.fileLines())
	}
}

func TestDatabaseAndSchemaSearch(t *testing.T) {
	m := testModel(t)
	m.envPW[sampleConn().ID()] = "pw"
	m.focus = paneDBs
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = asModel(t, tm)
	for _, r := range []rune{'e', 'p'} {
		tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = asModel(t, tm)
	}
	if db, ok := m.currentDB(); !ok || db != "epbt" {
		t.Fatalf("filtered db = %q ok=%v", db, ok)
	}
	if !strings.Contains(m.View(), "databases /ep") {
		t.Fatal(m.View())
	}
	if strings.Contains(m.View(), "postgres") {
		t.Fatal("unmatched database should be hidden")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, tm)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if !m.schemaMode {
		t.Fatal("enter should list schemas")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = asModel(t, tm)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = asModel(t, tm)
	if schema, ok := m.currentSchema(); !ok || schema != "app" {
		t.Fatalf("filtered schema = %q ok=%v", schema, ok)
	}
	if db, ok := m.currentDB(); !ok || db != "epbt" {
		t.Fatalf("schema search dropped the open database: %q ok=%v", db, ok)
	}
	if strings.Contains(m.View(), "public") {
		t.Fatal(m.View())
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if m.dbSearch || m.tableMode || m.dbQuery != "a" {
		t.Fatalf("enter should keep the filter: search=%v table=%v query=%q", m.dbSearch, m.tableMode, m.dbQuery)
	}
	if schema, ok := m.currentSchema(); !ok || schema != "app" {
		t.Fatalf("filtered schema after confirm = %q ok=%v", schema, ok)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, tm)
	if !m.tableMode || m.tableSchema != "app" || m.tableDB != "epbt" {
		t.Fatalf("tables mode=%v schema=%q db=%q", m.tableMode, m.tableSchema, m.tableDB)
	}
}

func TestFileListShowsModifiedTime(t *testing.T) {
	dir := t.TempDir()
	older := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)
	newer := time.Now().Add(-time.Minute).Truncate(time.Minute)
	if err := os.WriteFile(filepath.Join(dir, "old.sql"), []byte("-- old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.sql"), []byte("-- new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "old.sql"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "new.sql"), newer, newer); err != nil {
		t.Fatal(err)
	}
	files, err := discover.Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "new.sql" || files[1] != "old.sql" {
		t.Fatalf("newest first = %#v", files)
	}
	m := testModel(t)
	m.cwd = dir
	m.files = files
	m.focus = paneFiles
	view := m.View()
	if !strings.Contains(view, "new.sql") || !strings.Contains(view, newer.Format("02 Jan 15:04")) {
		t.Fatal(view)
	}
	if !strings.Contains(view, older.Format("02 Jan 15:04")) {
		t.Fatal(view)
	}
}

func TestCreateDatabaseSelectsExisting(t *testing.T) {
	m := testModel(t)
	m.memPW[sampleConn().ID()] = ""
	called := false
	ensureDB = func(context.Context, config.Connection, string, db.LogFunc) error {
		called = true
		return nil
	}
	tm, _ := m.finishCreateDB("epbt")
	m = asModel(t, tm)
	if called {
		t.Fatal("should not create an already listed database")
	}
	if db, _ := m.currentDB(); db != "epbt" {
		t.Fatal(db)
	}
}
