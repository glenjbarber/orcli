package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SearchOrder is the list of paths a configuration file is looked for at, in
// order. The first match wins and the search does not merge.
//
// Merging would be the friendlier behavior and it is the wrong one for a file
// holding a credential. A merge produces a file that parses and is wrong, and
// a reader has no way to tell that from a file that is right. The first match
// is what the reader wrote, or the most local thing they wrote, and anything
// else is a guess.
var SearchOrder = []string{
	"~/.orcli.json",
	"~/.config/orcli/orcli.json",
}

// FileMode is the mode the configuration file must carry.
//
// A `0755` parent directory is not itself an exposure and is not checked. The
// file is what holds the credential, and its mode is what governs it.
const FileMode fs.FileMode = 0o600

// Config is the configuration as read.
//
// Every field is optional except the API key, and the key is reported through
// ErrNoAPIKey rather than being an empty string, because an absent key and a
// key set to "" are the same thing to the reader and only one of them is a
// first-run state.
type Config struct {
	// APIKey is the credential. It is never read from the environment.
	APIKey string `json:"api_key,omitempty"`

	// Model is the model identifier a session starts with.
	Model string `json:"model,omitempty"`

	// Provider is the endpoint host, defaulting to the OpenRouter one.
	Provider string `json:"provider,omitempty"`

	// Mouse reports whether mouse reporting is on.
	Mouse bool `json:"mouse,omitempty"`

	// Bell reports whether the terminal bell is rung when a reply arrives.
	Bell bool `json:"bell,omitempty"`

	// Color reports whether colour output is on. Off is the default, and is
	// decided by this file and by /color alone: NO_COLOR and every other
	// environment variable are ignored, on the same terms as the API key.
	Color bool `json:"color,omitempty"`

	// Verbosity is how much the model is asked to answer with, 0 to 6.
	Verbosity int `json:"verbosity,omitempty"`

	// Approval is the tool approval mode: ask, allow, or refuse.
	Approval string `json:"approval,omitempty"`

	// Trusted is the list of directories that have been trusted.
	Trusted []string `json:"OPENROUTER_TRUSTED,omitempty"`
}

// Default is the configuration a session runs with when nothing is on disk.
//
// The credential is empty, which Load reports as ErrNoAPIKey rather than as a
// configuration that is valid and silent. Every other field is the value the
// design names as the default.
func Default() Config {
	return Config{
		Model:     "",
		Provider:  "openrouter.ai",
		Mouse:     false,
		Bell:      false,
		Color:     false,
		Verbosity: 0,
		Approval:  "ask",
	}
}

// Load reads the first configuration file found on the search order.
//
// Four outcomes are distinguished, because they are four different things for a
// reader to be told:
//
//   - the file is read and carries a credential, and nil is returned;
//   - the file is read and carries no credential, and ErrNoAPIKey is returned
//     along with the configuration that was read;
//   - no file exists anywhere on the search order, and ErrNotFound is returned;
//   - the file exists and is wrong, which is fatal: a bad mode, a malformed
//     body, a top level that is not an object, or a path that is a directory.
func Load() (Config, error) {
	for _, path := range SearchOrder {
		resolved, err := resolve(path)
		if err != nil {
			return Config{}, err
		}

		data, err := os.ReadFile(resolved)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Config{}, fmt.Errorf("config: read %s: %w", resolved, err)
		}
		return Parse(data, resolved)
	}
	return Config{}, ErrNotFound
}

// Parse reads a configuration from bytes.
//
// The path is used only for the diagnostics, so a caller holding the contents
// from somewhere other than disk still gets a failure that names the file.
func Parse(data []byte, path string) (Config, error) {
	if err := checkMode(path); err != nil {
		return Config{}, err
	}

	if !isObject(data) {
		return Config{}, fmt.Errorf("config: %s: %w", path, ErrNotAnObject)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg := Default()
	if err := cfg.decode(raw); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", path, err)
	}

	// The credential is the one absence that is not fatal. Everything else in
	// the file is a preference, and a missing preference has a default that is
	// better than a refusal.
	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("config: %s: %w", path, ErrNoAPIKey)
	}
	return cfg, nil
}

// isObject reports whether data is a JSON object at its top level.
//
// The check is made before decoding rather than by inspecting a decode failure,
// because an array or a bare number is valid JSON and fails to decode into a
// map with a type error that reads as a malformed file. A reader who wrote a
// bare number deserves to be told the shape is wrong, not that their file is
// corrupt: one has a fix the reader can see, the other sends them looking for
// a quoting mistake that is not there.
func isObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{'
}

// decode reads the fields this client names, and ignores the rest.
//
// An unknown key is skipped rather than refused. The endpoint and the reader
// both write this file, and a file written by a newer version must still open
// in an older client rather than locking a reader out of their own
// configuration because of a key they do not recognize.
func (c *Config) decode(raw map[string]json.RawMessage) error {
	for key, value := range raw {
		var err error
		switch key {
		case "api_key":
			err = readString(value, &c.APIKey)
		case "model":
			err = readString(value, &c.Model)
		case "provider":
			err = readString(value, &c.Provider)
		case "mouse":
			err = readBool(value, &c.Mouse)
		case "bell":
			err = readBool(value, &c.Bell)
		case "color":
			err = readBool(value, &c.Color)
		case "verbosity":
			err = readInt(value, &c.Verbosity)
		case "approval":
			err = readString(value, &c.Approval)
		case "OPENROUTER_TRUSTED":
			err = readStrings(value, &c.Trusted)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return nil
}

// checkMode refuses a file that is readable by anyone but its owner.
//
// The check is on the file alone and not on the parent directory, since a
// permissive directory does not by itself expose the contents of a file inside
// it, and refusing on that basis would lock a reader out of a perfectly sound
// configuration.
func checkMode(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("config: %s: %w", path, ErrNotFound)
		}
		return fmt.Errorf("config: stat %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("config: %s is a directory, not a configuration file", path)
	}
	if perm := info.Mode().Perm(); perm != FileMode {
		return fmt.Errorf("config: %s is mode %#o, want %#o: %w",
			path, perm, FileMode, ErrBadMode)
	}
	return nil
}

// resolve expands a leading `~` and refuses a relative result.
//
// XDG_CONFIG_HOME is honoured, but a relative value for it is not. The
// specification requires an absolute path, and resolving a relative one
// against the working directory would read a credential out of a directory
// another account wrote.
func resolve(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("config: find the home directory: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("config: %s is not an absolute path", path)
	}
	return path, nil
}

// readString reads a JSON string into dst, and treats null as absent.
//
// Absent and empty are the same thing to a reader, so a null is not an error.
func readString(raw json.RawMessage, dst *string) error {
	if isNull(raw) {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// readBool reads a JSON boolean into dst.
func readBool(raw json.RawMessage, dst *bool) error {
	if isNull(raw) {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// readInt reads a JSON number into dst, and accepts a quoted one.
//
// The endpoint quotes figures that the reader wrote by hand, and a preference
// that a reader typed as "2" is the same preference as one they typed as 2.
func readInt(raw json.RawMessage, dst *int) error {
	if isNull(raw) {
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	v, err := n.Int64()
	if err != nil {
		return err
	}
	*dst = int(v)
	return nil
}

// readStrings reads a JSON array of strings into dst, and treats null as absent.
//
// An entry that is not a string is an error rather than a dropped entry. A
// trusted list is a list of directories, and a list that quietly lost one is a
// directory that will be asked about again.
func readStrings(raw json.RawMessage, dst *[]string) error {
	if isNull(raw) {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// isNull reports whether a raw value is JSON null.
func isNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
