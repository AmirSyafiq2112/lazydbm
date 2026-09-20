package tui

import (
	"path/filepath"
	"testing"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

func TestVisibleWindow(t *testing.T) {
	start, end := visibleWindow(10, 0, 5)
	if start != 0 || end != 5 {
		t.Fatalf("start=%d end=%d", start, end)
	}
	start, end = visibleWindow(10, 9, 5)
	if end != 10 || start != 5 {
		t.Fatalf("start=%d end=%d", start, end)
	}
}

func TestMoveIndex(t *testing.T) {
	if moveIndex(0, 3, "j") != 1 {
		t.Fatal("j should increment")
	}
	if moveIndex(0, 3, "k") != 0 {
		t.Fatal("k at top stays")
	}
	if moveIndex(1, 3, "G") != 2 {
		t.Fatal("G should go last")
	}
}

func TestIndexByID(t *testing.T) {
	conns := []config.Connection{{
		Engine: config.EnginePostgres, Host: "h", Port: 1, User: "u", Database: "d",
	}}
	if indexByID(conns, conns[0].ID()) != 0 {
		t.Fatal("expected index 0")
	}
	if indexByID(conns, "nope") != -1 {
		t.Fatal("expected -1")
	}
}

func TestExportAbs(t *testing.T) {
	got := exportAbs("/tmp", "out.sql")
	if got != filepath.Join("/tmp", "out.sql") {
		t.Fatalf("got %q", got)
	}
	if exportAbs("/tmp", "/abs/out.sql") != "/abs/out.sql" {
		t.Fatal("abs path should pass through")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Fatal("no truncate")
	}
	got := truncate("hello world", 8)
	if got != "hello w…" {
		t.Fatalf("got %q", got)
	}
}
