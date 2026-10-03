package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoAPIKey is returned when the configuration carries no credential.
//
// It is not a fatal condition, and the difference matters. A reader with no key
// can still open the interface, read what they have written before, and be told
// what is missing. Refusing to start would leave nothing on screen explaining
// why.
var ErrNoAPIKey = errors.New("config: no API key configured")

// ErrMode is returned when the configuration file is readable by anyone but its
// owner.
//
// It is a hard failure rather than a warning, because a permissive mode leaks
// the credential silently and the leak is discovered by someone else.
var ErrMode = errors.New("config: the file must be mode 0600")

// requiredMode is the only mode the file may carry.
const requiredMode fs.FileMode = 0o600

// Config is what the configuration file holds.
//
// The fields are exported and the format is JSON, so a field added here is a
// field a reader can set by hand. That is deliberate: a file holding a
// credential is not a thing to be edited by a program that did not write it,
// and a hand-written key works without any special support.
type Config struct {
	// APIKey is the credential. It is never read from the environment.
	APIKey string `json:"OPENROUTER_API_KEY"`

	// Model is the model a session starts with. An empty value means the
	// interface opens and asks rather than refusing.
	Model string `json:"model,omitempty"`

	// Host is the endpoint. Empty means the endpoint the client already uses.
	Host string `json:"host,omitempty"`

	// Color is tri-state: absent leaves the choice to the interface, which
	// decides from this flag and the theme and nowhere else.
	Color *bool `json:"color,omitempty"`

	// Bell rings the terminal when a reply has finished arriving.
	Bell bool `json:"bell,omitempty"`

	// Mouse turns on mouse reporting for wheel scrolling.
	Mouse bool `json:"mouse,omitempty"`

	// Trusted is the set of directories a session has been approved to run in.
	// An approval is recorded here rather than in a marker file, because it is
	// a property of the configuration and nothing else.
	Trusted []string `json:"OPENROUTER_TRUSTED,omitempty"`
}

// Default is the configuration installed on first run.
//
// The key is deliberately empty. A file that carried a placeholder would
// suppress the first-time setup that tells a reader what to do, and a file that
// carried a real key would put a credential in a repository.
func Default() Config {
	return Config{}
}

// installDefault writes the default configuration, and only when it is absent.
//
// The file is created exclusively at 0600. An existing file is left exactly as
// it is, whatever state it is in. No merge is attempted: a partial merge of a
// credential file can produce a file that parses but is wrong, and doing nothing
// is the reversible outcome. A reader whose file is in an unexpected state is
// told about it rather than having it silently corrected.
func installDefault(path string) error {
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode the default: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, requiredMode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return fmt.Errorf("config: create %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	// The mode is set again rather than relied on from the open. O_EXCL with a
	// mode is subject to the process umask, and the result should not depend on
	// what the reader happens to have set.
	if err := f.Chmod(requiredMode); err != nil {
		return fmt.Errorf("config: set the mode on %s: %w", path, err)
	}
	return nil
}

// Load reads the configuration.
//
// The search order is ~/.orcli.json, then ~/.config/orcli/orcli.json with
// XDG_CONFIG_HOME honoured. The search does not merge: the first match wins.
// Merging two files that both claim to be the configuration is a way to have no
// configuration, since the reader cannot tell which one answered.
//
// A missing key is not fatal. ErrNoAPIKey carries the rest of what the file
// held out of it, so the caller can stand in a configuration and open the
// interface anyway. Anything else, including a wrong file mode, is fatal,
// because a fault is not a state.
//
// The boolean reports whether a file was found at all, so the caller can install
// the default without asking the question twice.
func Load() (Config, bool, error) {
	var cfg Config

	path, ok, err := find()
	if err != nil {
		return cfg, false, err
	}
	if !ok {
		return cfg, false, nil
	}

	if err := checkMode(path); err != nil {
		return cfg, false, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, false, fmt.Errorf("config: read %s: %w", path, err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, false, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if strings.TrimSpace(cfg.APIKey) == "" {
		return cfg, true, ErrNoAPIKey
	}
	return cfg, true, nil
}

// find returns the path of the configuration file, and whether one exists.
//
// A directory at the configuration path is an error rather than an absence. A
// path that is not a file cannot be read, and reporting it as no configuration
// would leave a reader installing a default into a directory.
func find() (string, bool, error) {
	var candidates []string

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".orcli.json"))
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			// A relative value is ignored rather than resolved. The
			// specification requires an absolute path, and resolving a
			// relative one against the working directory would read a
			// credential out of a directory someone else wrote.
			if filepath.IsAbs(xdg) {
				candidates = append(candidates,
					filepath.Join(xdg, "orcli", "orcli.json"))
			}
		} else {
			candidates = append(candidates,
				filepath.Join(home, ".config", "orcli", "orcli.json"))
		}
	}

	for _, path := range candidates {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", false, fmt.Errorf("config: read %s: %w", path, err)
		}
		if info.IsDir() {
			return "", false, fmt.Errorf("config: %s is a directory, not a file", path)
		}
		return path, true, nil
	}

	return "", false, nil
}

// checkMode refuses a configuration file readable by anyone but its owner.
//
// The check is on the file alone. A 0755 parent directory is not itself an
// exposure, since the file carrying the mode is the only one whose permissions
// say anything about who can read the key.
func checkMode(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if info.Mode().Perm() != requiredMode {
		return fmt.Errorf("%w: %s is %04o", ErrMode, path, info.Mode().Perm())
	}
	return nil
}

// InstallDefault writes the default configuration if it is absent.
//
// It is one of the three writers, and the only one that creates the file. A
// file made by a command would hold no key, and would suppress the first-time
// setup that tells a reader what to do.
func InstallDefault() error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return errors.New("config: the home directory could not be determined")
	}
	return installDefault(filepath.Join(home, ".orcli.json"))
}

// Path returns the path of the configuration file, for a message that names it.
//
// It is empty when no file exists. A message that says where the file would go
// is more useful to a reader than one that says nothing, so an empty path here
// is paired with the first of the two candidates rather than with silence.
func Path() string {
	path, _, _ := find()
	return path
}
