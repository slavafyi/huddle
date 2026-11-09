package store

import (
	"path/filepath"
	"time"

	"github.com/slavafyi/cuddle/internal/clickup"
)

// CacheVersion is the current cache file format.
const CacheVersion = 1

// Cache is data fetched from ClickUp and GitHub. It is safe to delete.
type Cache struct {
	Version       int            `json:"version"`
	Me            clickup.User   `json:"me"`
	Tasks         []clickup.Task `json:"tasks"`
	TasksSyncedAt time.Time      `json:"tasks_synced_at,omitzero"`
	TasksFullAt   time.Time      `json:"tasks_full_at,omitzero"`
}

// CachePath is the default cache file.
func CachePath() string { return filepath.Join(CacheDir(), "cache.json") }

// StatePath is the default state file.
func StatePath() string { return filepath.Join(StateDir(), "state.json") }
