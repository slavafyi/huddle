package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type doc struct {
	Version int      `json:"version"`
	Items   []string `json:"items"`
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "x.json")
	if err := Save(p, doc{Version: 1, Items: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	var got doc
	if err := Load(p, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Items) != 1 || got.Items[0] != "a" {
		t.Fatalf("got %+v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestLoadMissingIsNotAnError(t *testing.T) {
	var got doc
	if err := Load(filepath.Join(t.TempDir(), "none.json"), &got); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCorruptIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte("{"), 0o644)
	var got doc
	if err := Load(p, &got); err == nil {
		t.Fatal("want error")
	}
}

func TestWriterSkipsOlderGenerations(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	var w Writer
	if err := w.Write(p, []byte(`{"v":2}`), 2); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(p, []byte(`{"v":1}`), 1); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != `{"v":2}` {
		t.Errorf("older snapshot overwrote newer: %s", data)
	}
}

func TestProfilePaths(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/c")
	t.Setenv("XDG_STATE_HOME", "/s")
	if got := CachePath("work"); got != "/c/huddle/work/cache.json" {
		t.Errorf("cache = %q", got)
	}
	if got := StatePath("work"); got != "/s/huddle/work/state.json" {
		t.Errorf("state = %q", got)
	}
}

func TestRememberProfile(t *testing.T) {
	dir := t.TempDir()
	if got := LastProfile(dir); got != "" {
		t.Errorf("last = %q before any use", got)
	}
	for _, p := range []string{"a", "b", "c", "a"} {
		if err := RememberProfile(dir, p); err != nil {
			t.Fatal(err)
		}
	}
	if got := LastProfile(dir); got != "a" {
		t.Errorf("last = %q", got)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "last_profile"))
	if string(data) != "a\n" {
		t.Errorf("last_profile file = %q", data)
	}
	if got := RecentProfiles(dir); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Errorf("recent = %v", got)
	}
	for i := range 20 {
		RememberProfile(dir, string(rune('d'+i)))
	}
	if got := RecentProfiles(dir); len(got) != maxRecentProfiles {
		t.Errorf("recent not capped: %v", got)
	}
}
