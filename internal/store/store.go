// Package store reads and writes cuddle's JSON cache and state files.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// CacheDir returns $XDG_CACHE_HOME/cuddle or ~/.cache/cuddle.
func CacheDir() string { return xdg("XDG_CACHE_HOME", ".cache") }

// StateDir returns $XDG_STATE_HOME/cuddle or ~/.local/state/cuddle.
func StateDir() string { return xdg("XDG_STATE_HOME", ".local/state") }

func xdg(env, fallback string) string {
	dir := os.Getenv(env)
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, fallback)
	}
	return filepath.Join(dir, "cuddle")
}
