package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildHuddle builds the binary with extra ldflags and returns its path.
func buildHuddle(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "huddle")
	out, err := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// -version prints the version set at build time and exits, without
// reading the config.
func TestVersionFlag(t *testing.T) {
	bin := buildHuddle(t, "-X main.version=1.2.3")
	out, err := exec.Command(bin, "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("huddle -version: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "huddle 1.2.3" {
		t.Errorf("huddle -version = %q, want %q", got, "huddle 1.2.3")
	}
}

// A plain go build has no version set and says "dev".
func TestVersionFlagDefault(t *testing.T) {
	bin := buildHuddle(t, "")
	out, err := exec.Command(bin, "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("huddle -version: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "huddle dev" {
		t.Errorf("huddle -version = %q, want %q", got, "huddle dev")
	}
}
