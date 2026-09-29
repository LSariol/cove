// Package bootstrap manages the marker file that locks the one-time bootstrap
// endpoint. While the marker exists, the endpoint refuses every request.
package bootstrap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const markerName = "bootstrap_completed"

// ErrLocked is returned by Lock when the marker already exists.
var ErrLocked = errors.New("bootstrap is already locked")

type Marker struct {
	dir string
}

// NewMarker returns a Marker whose file lives in dir.
func NewMarker(dir string) *Marker {
	return &Marker{dir: dir}
}

// Lock creates the marker. It returns ErrLocked if the marker already exists,
// so only one caller can claim the bootstrap. Any other error means the marker
// couldn't be created at all (permissions, disk).
func (m *Marker) Lock() error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return fmt.Errorf("failed to create marker directory: %w", err)
	}

	f, err := os.OpenFile(m.path(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return ErrLocked
	}
	if err != nil {
		return fmt.Errorf("failed to create marker file: %w", err)
	}
	_ = f.Close()

	return nil
}

// Clear removes the marker, reopening the bootstrap endpoint.
func (m *Marker) Clear() error {
	if err := os.Remove(m.path()); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove marker file: %w", err)
		}
	}

	return nil
}

// Locked reports whether the marker exists, i.e. the bootstrap endpoint is
// closed.
func (m *Marker) Locked() (bool, error) {
	_, err := os.Stat(m.path())
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (m *Marker) path() string {
	return filepath.Join(m.dir, markerName)
}
