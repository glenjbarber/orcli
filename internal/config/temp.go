package config

import (
	"errors"
	"os"
	"path/filepath"
)

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
