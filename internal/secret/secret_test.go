package secret

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMemoryStore(t *testing.T) {
	s := NewMemory()
	if _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
	if err := s.Set("id", "pw"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("id")
	if err != nil || got != "pw" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := s.Delete("id"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := s.Delete("id"); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultWrappersUseStore(t *testing.T) {
	orig := Default
	mem := NewMemory()
	Default = mem
	t.Cleanup(func() { Default = orig })

	if err := Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	got, err := Get("k")
	if err != nil || got != "v" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := Delete("k"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestMapKeyringErr(t *testing.T) {
	if mapKeyringErr(nil) != nil {
		t.Fatal("nil should stay nil")
	}
	if !errors.Is(mapKeyringErr(keyring.ErrNotFound), ErrNotFound) {
		t.Fatal("keyring not found should map")
	}
	other := errors.New("boom")
	if mapKeyringErr(other) != other {
		t.Fatal("other errors pass through")
	}
}

func TestMapKeyringDeleteErr(t *testing.T) {
	if mapKeyringDeleteErr(nil) != nil {
		t.Fatal("nil")
	}
	if mapKeyringDeleteErr(keyring.ErrNotFound) != nil {
		t.Fatal("not found is ok")
	}
	other := errors.New("boom")
	if mapKeyringDeleteErr(other) != other {
		t.Fatal("pass through")
	}
}
