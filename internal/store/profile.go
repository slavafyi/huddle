package store

import (
	"os"
	"path/filepath"
	"strings"
)

// maxRecentProfiles is how many profiles the recent list keeps.
const maxRecentProfiles = 10

// LastProfile returns the last used profile recorded in dir, or "".
func LastProfile(dir string) string {
	data, _ := os.ReadFile(filepath.Join(dir, "last_profile"))
	return strings.TrimSpace(string(data))
}

// RecentProfiles returns the profiles used from dir, most recent first.
func RecentProfiles(dir string) []string {
	data, _ := os.ReadFile(filepath.Join(dir, "recent_profiles"))
	return strings.Fields(string(data))
}

// RememberProfile records profile as the last used one and moves it to the
// front of the recent list.
func RememberProfile(dir, profile string) error {
	list := []string{profile}
	for _, p := range RecentProfiles(dir) {
		if p != profile && len(list) < maxRecentProfiles {
			list = append(list, p)
		}
	}
	if err := writeFile(filepath.Join(dir, "last_profile"), []byte(profile+"\n")); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "recent_profiles"), []byte(strings.Join(list, "\n")+"\n"))
}
