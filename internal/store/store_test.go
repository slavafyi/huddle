package store

import (
	"os"
	"path/filepath"
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
