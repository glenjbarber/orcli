package config

import (
	"path/filepath"
	"testing"
)

// TestTempDirIsUnderTheConfigDirectory covers the one thing this file promises: the
// handoff directory sits beside ~/.orcli.json and the cognito marker, not in whatever
// the OS calls its temp path.
func TestTempDirIsUnderTheConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := TempDir()
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	want := filepath.Join(home, ".orcli", "temp")
	if got != want {
		t.Fatalf("TempDir() = %s, want %s", got, want)
	}
}
