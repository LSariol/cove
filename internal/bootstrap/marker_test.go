package bootstrap

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLockOnlySucceedsOnce(t *testing.T) {
	m := NewMarker(filepath.Join(t.TempDir(), "markers"))

	if err := m.Lock(); err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if err := m.Lock(); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock = %v, want ErrLocked", err)
	}
}

func TestClearReopens(t *testing.T) {
	m := NewMarker(t.TempDir())

	if err := m.Clear(); err != nil {
		t.Fatalf("Clear with no marker: %v", err)
	}
	if err := m.Lock(); err != nil {
		t.Fatal(err)
	}
	if err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := m.Lock(); err != nil {
		t.Fatalf("Lock after Clear: %v", err)
	}
}
