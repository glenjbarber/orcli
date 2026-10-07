package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDirIsHomeDotOrcli covers the path every subsystem beneath it agrees on.
func TestDirIsHomeDotOrcli(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(home, ".orcli")
	if got != want {
		t.Fatalf("Dir() = %s, want %s", got, want)
	}
}

// TestEnsureDirCreatesIt covers what main.go relies on at startup: the
// directory exists afterward, whether or not anything else ever writes
// beneath it this session.
func TestEnsureDirCreatesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err := os.Stat(filepath.Join(home, ".orcli"))
	if err != nil {
		t.Fatalf("stat ~/.orcli: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("~/.orcli exists but is not a directory")
	}
}

// TestEnsureDirIsIdempotent covers the ordinary case: a session that is not
// the reader's first finds the directory already there and does nothing
// worse than confirm it.
func TestEnsureDirIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureDir(); err != nil {
		t.Fatalf("first EnsureDir: %v", err)
	}
	if err := EnsureDir(); err != nil {
		t.Fatalf("second EnsureDir: %v", err)
	}
}

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
