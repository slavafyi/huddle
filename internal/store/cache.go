package store

import (
	"path/filepath"
	"time"

	"github.com/slavafyi/huddle/internal/github"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
)

// CacheVersion is the current cache file format. Version 2 holds
// source-neutral tasks; version 3 adds PR closing issues and repo remotes.
const CacheVersion = 3

// Cache is data fetched from task sources and GitHub. It is safe to delete.
type Cache struct {
	Version       int              `json:"version"`
	Me            map[string]int64 `json:"me"` // my user ID, by source
	Tasks         []task.Task      `json:"tasks"`
	TasksSyncedAt time.Time        `json:"tasks_synced_at,omitzero"`
	TasksFullAt   time.Time        `json:"tasks_full_at,omitzero"`
	Repos         []workspace.Repo `json:"repos"`
	ReposAt       time.Time        `json:"repos_at,omitzero"`
	PRs           []github.PR      `json:"prs"`
	PRsAt         time.Time        `json:"prs_at,omitzero"`
}

// CachePath is a profile's cache file.
func CachePath(profile string) string { return filepath.Join(CacheDir(), profile, "cache.json") }

// StatePath is a profile's state file.
func StatePath(profile string) string { return filepath.Join(StateDir(), profile, "state.json") }
