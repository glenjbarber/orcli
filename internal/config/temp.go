package config

import (
	"errors"
	"os"
	"path/filepath"
)

// Dir is orcli's own configuration directory, ~/.orcli. TempDir, the saved
// sessions directory (internal/saved.DefaultDir), and cognito's file all sit
// beneath it.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("config: the home directory could not be determined")
	}
	return filepath.Join(home, ".orcli"), nil
}

// EnsureDir creates Dir if it does not already exist.
//
// Every subsystem that writes beneath Dir - saved sessions, TempDir, cognito
// - already creates its own subdirectory with MkdirAll on its first write, so
// this is not load-bearing for any of them. It exists so ~/.orcli is there
// from the moment orcli starts rather than only appearing once a session
// happens to save, begin, or touch cognito - Glen asked for exactly that
// (2026-10-07), rather than a directory whose existence depends on what a
// session happened to do.
func EnsureDir() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

// TempDir is where orcli writes files that exist only for the lifetime of one
// operation, such as a `/begin` handoff note on its way from a save to a load.
//
// It is a subdirectory of orcli's own configuration directory rather than the
// OS default temporary directory, so a file written here is never shared with
// every other program on the machine, is cleaned up by the one piece of code
// that wrote it rather than by whatever the OS eventually sweeps, and sits
// beside the sessions directory and ~/.orcli.json rather than in an unrelated
// place a reader would have to be told about separately.
//
// It is derived from the same home directory DefaultPath resolves ~/.orcli.json
// against, so the two never disagree about which account's files they are.
func TempDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("config: the home directory could not be determined")
	}
	return filepath.Join(home, ".orcli", "temp"), nil
}
