// Package store reads and writes huddle's JSON cache and state files.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Load decodes the JSON file at path into v. A missing file leaves v as is.
func Load(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// Save writes v as JSON to path atomically: it writes a temp file in the same
// folder and renames it.
func Save(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

// Writer writes snapshots in the background without letting an older
// snapshot replace a newer one. Callers marshal on their own goroutine (so
// the snapshot can't change underneath) and number snapshots in order.
type Writer struct {
	mu      sync.Mutex
	written map[string]uint64
}

// Write saves data to path unless a later generation was already written.
func (w *Writer) Write(path string, data []byte, gen uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.written == nil {
		w.written = map[string]uint64{}
	}
	if gen <= w.written[path] {
		return nil
	}
	if err := writeFile(path, data); err != nil {
		return err
	}
	w.written[path] = gen
	return nil
}

func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// CacheDir returns $XDG_CACHE_HOME/huddle or ~/.cache/huddle.
func CacheDir() string { return xdg("XDG_CACHE_HOME", ".cache") }

// StateDir returns $XDG_STATE_HOME/huddle or ~/.local/state/huddle.
func StateDir() string { return xdg("XDG_STATE_HOME", ".local/state") }

func xdg(env, fallback string) string {
	dir := os.Getenv(env)
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, fallback)
	}
	return filepath.Join(dir, "huddle")
}
