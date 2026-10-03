package config

import (
	"encoding/json"
	"os"
)

// marshalIndent renders a configuration the way it is written to disk.
//
// It is used by the writers and by the tests, so the shape on disk is decided
// in one place rather than by each caller.
func marshalIndent(cfg Config) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// osWriteFile writes data to path at the only mode a credential file may carry.
//
// The mode is set twice for the reason stated on installDefault: an open with a
// mode is subject to the umask, and the result should not depend on it.
func osWriteFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// statFile returns the file information at path.
func statFile(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
