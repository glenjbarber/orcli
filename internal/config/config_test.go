package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// home is a temporary home directory, with every environment variable this
// package reads set to point inside it.
//
// HOME is set rather than whatever the reader has, so a test cannot read the
// real configuration file of whoever is running it. XDG_CONFIG_HOME is set to
// an absolute path inside the temporary directory for the same reason.
func home(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	return dir
}

// writeConfig writes a configuration at the given path and sets its mode.
func writeConfig(t *testing.T, path string, body string, mode fs.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
}

// TestLoadReadsTheKey covers the ordinary path.
func TestLoadReadsTheKey(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), `{"OPENROUTER_API_KEY":"sk-or-v1-abc"}`, 0o600)

	cfg, found, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Error("found = false, want true: the file exists")
	}
	if got, want := cfg.APIKey, "sk-or-v1-abc"; got != want {
		t.Errorf("APIKey = %q, want %q", got, want)
	}
}

// TestLoadIgnoresTheEnvironmentKey is the rule that matters most in this
// package.
//
// A key in the environment is ignored even when set. The class of failure this
// removes is a correct file shadowed by a stale value elsewhere, and a reader
// who is told to set a variable that is never read.
func TestLoadIgnoresTheEnvironmentKey(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), `{"OPENROUTER_API_KEY":"sk-or-v1-from-file"}`, 0o600)

	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-from-environment")

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.APIKey, "sk-or-v1-from-file"; got != want {
		t.Errorf("APIKey = %q, want %q: the file is the only source", got, want)
	}
}

// TestLoadIgnoresTheEnvironmentKeyWhenTheFileHasNone covers the other half.
//
// A key in the environment is not a key at all, so a file without one is a
// session without one, and the reader is told so rather than silently given a
// credential from somewhere they did not choose.
func TestLoadIgnoresTheEnvironmentKeyWhenTheFileHasNone(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), `{"model":"some/model"}`, 0o600)

	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-from-environment")

	_, found, err := Load()
	if !found {
		t.Fatal("found = false, want true: the file exists")
	}
	if !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Load returned %v, want ErrNoAPIKey", err)
	}
}

// TestLoadCarriesTheRestOfTheFileOnAMissingKey covers what ErrNoAPIKey has to
// deliver.
//
// The caller stands in a configuration so the interface still opens, and that
// only works if the rest of the file came out with the error.
func TestLoadCarriesTheRestOfTheFileOnAMissingKey(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"),
		`{"model":"some/model","bell":true,"OPENROUTER_TRUSTED":["/tmp/one"]}`, 0o600)

	cfg, found, err := Load()
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("Load returned %v, want ErrNoAPIKey", err)
	}
	if !found {
		t.Error("found = false, want true: a file without a key is still a file")
	}
	if got, want := cfg.Model, "some/model"; got != want {
		t.Errorf("Model = %q, want %q", got, want)
	}
	if !cfg.Bell {
		t.Error("Bell = false, want true: the rest of the file is carried out")
	}
	if len(cfg.Trusted) != 1 || cfg.Trusted[0] != "/tmp/one" {
		t.Errorf("Trusted = %v, want [/tmp/one]", cfg.Trusted)
	}
}

// TestLoadRefusesAPermissiveMode covers the hard failure.
//
// A permissive mode leaks the credential silently, so it is refused at startup
// with the mode named, rather than warned about and used.
func TestLoadRefusesAPermissiveMode(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"),
		`{"OPENROUTER_API_KEY":"sk-or-v1-abc"}`, 0o644)

	_, _, err := Load()
	if !errors.Is(err, ErrMode) {
		t.Fatalf("Load returned %v, want ErrMode", err)
	}
	if !strings.Contains(err.Error(), "0644") {
		t.Errorf("failure is %q, want it to name the mode", err)
	}
}

// TestLoadRefusesEveryOtherMode checks the mode is exact rather than a
// minimum.
//
// 0400 is more restrictive than required and is refused anyway: the rule is one
// mode, and a file that does not carry it is a file whose mode was set by
// something other than this client.
func TestLoadRefusesEveryOtherMode(t *testing.T) {
	for _, mode := range []fs.FileMode{0o400, 0o640, 0o604, 0o660, 0o606, 0o700} {
		t.Run(mode.String(), func(t *testing.T) {
			h := home(t)
			writeConfig(t, filepath.Join(h, ".orcli.json"),
				`{"OPENROUTER_API_KEY":"sk-or-v1-abc"}`, mode)

			if _, _, err := Load(); !errors.Is(err, ErrMode) {
				t.Errorf("Load accepted mode %04o, want it refused", mode)
			}
		})
	}
}

// TestLoadReportsNoFileAsAnAbsence covers the first run, which is not a fault.
func TestLoadReportsNoFileAsAnAbsence(t *testing.T) {
	home(t)

	_, found, err := Load()
	if err != nil {
		t.Errorf("Load returned %v, want nil for a first run", err)
	}
	if found {
		t.Error("found = true, want false: there is no file")
	}
}

// TestLoadReportsADirectoryAsAnError covers the case that is not an absence.
//
// A directory at the configuration path cannot be read, and reporting it as no
// configuration would leave the caller installing a default into a directory.
func TestLoadReportsADirectoryAsAnError(t *testing.T) {
	h := home(t)
	if err := os.MkdirAll(filepath.Join(h, ".orcli.json"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, found, err := Load()
	if err == nil {
		t.Fatal("Load returned nil, want an error for a directory at the path")
	}
	if found {
		t.Error("found = true, want false: a directory is not a configuration")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("failure is %q, want it to say what was found", err)
	}
}

// TestLoadReportsUnreadableJSONAsAFault covers a file that is a fault rather
// than a state.
func TestLoadReportsUnreadableJSONAsAFault(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), `{not json`, 0o600)

	if _, _, err := Load(); err == nil {
		t.Error("Load returned nil, want an error for unreadable content")
	}
}

// TestLoadIgnoresAFieldItDoesNotKnow covers a file written by a later version.
//
// A configuration file is meant to be edited by hand, so refusing a file that
// carries a key this version has never heard of would make the file unusable
// in exactly the situation it exists for.
func TestLoadIgnoresAFieldItDoesNotKnow(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"),
		`{"OPENROUTER_API_KEY":"sk-or-v1-abc","somethingNew":{"a":1}}`, 0o600)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.APIKey, "sk-or-v1-abc"; got != want {
		t.Errorf("APIKey = %q, want %q", got, want)
	}
}

// TestLoadIgnoresARelativeXDGPath covers the rule about a relative value.
//
// The specification requires an absolute path. Resolving a relative one against
// the working directory would read a credential out of a directory someone else
// wrote, so a relative value names no file at all.
func TestLoadIgnoresARelativeXDGPath(t *testing.T) {
	h := home(t)
	t.Setenv("XDG_CONFIG_HOME", "relative/path")

	// The relative path is ignored, so a file only the relative path would have
	// found must not be read.
	writeConfig(t, filepath.Join(h, "relative", "path", "orcli", "orcli.json"),
		`{"OPENROUTER_API_KEY":"sk-or-v1-relative"}`, 0o600)

	if _, found, err := Load(); err != nil || found {
		t.Errorf("Load found %v with error %v, want it to ignore a relative path", found, err)
	}
}

// TestLoadPrefersTheFirstFile covers the search order, and the absence of a
// merge.
//
// The first match wins. Merging two files that both claim to be the
// configuration is a way to have no configuration.
func TestLoadPrefersTheFirstFile(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"),
		`{"OPENROUTER_API_KEY":"sk-or-v1-first"}`, 0o600)
	writeConfig(t, filepath.Join(h, "xdg", "orcli", "orcli.json"),
		`{"OPENROUTER_API_KEY":"sk-or-v1-second"}`, 0o600)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.APIKey, "sk-or-v1-first"; got != want {
		t.Errorf("APIKey = %q, want %q: the first match wins and there is no merge",
			got, want)
	}
}

// TestLoadFallsBackToTheSecondFile covers the rest of the order.
func TestLoadFallsBackToTheSecondFile(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, "xdg", "orcli", "orcli.json"),
		`{"OPENROUTER_API_KEY":"sk-or-v1-xdg"}`, 0o600)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.APIKey, "sk-or-v1-xdg"; got != want {
		t.Errorf("APIKey = %q, want %q", got, want)
	}
}

// TestInstallDefaultCreatesTheFileAtTheRightMode covers the only writer that
// creates.
func TestInstallDefaultCreatesTheFileAtTheRightMode(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")

	if err := InstallDefault(); err != nil {
		t.Fatalf("InstallDefault: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the default was not created: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode is %04o, want 0600", got)
	}
}

// TestInstallDefaultLeavesAnExistingFileAlone covers the no-merge decision.
//
// An existing file is left exactly as it is, whatever state it is in. A partial
// merge of a credential file can produce a file that parses but is wrong, and
// doing nothing is the reversible outcome.
func TestInstallDefaultLeavesAnExistingFileAlone(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")
	body := `{"OPENROUTER_API_KEY":"sk-or-v1-kept","somethingNew":{"a":1}}`
	writeConfig(t, path, body, 0o600)

	if err := InstallDefault(); err != nil {
		t.Fatalf("InstallDefault: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != body {
		t.Errorf("the file was changed to %s, want it left exactly as it was", data)
	}
}

// TestInstallDefaultCarriesNoKey covers why the default key is empty.
//
// A file carrying a placeholder would suppress the first-time setup that tells
// a reader what to do.
func TestInstallDefaultCarriesNoKey(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")

	if err := InstallDefault(); err != nil {
		t.Fatalf("InstallDefault: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("the installed default does not parse: %v", err)
	}
	if cfg.APIKey != "" {
		t.Errorf("the installed default carries the key %q, want it empty", cfg.APIKey)
	}
}

// TestInstalledDefaultLoadsAsNoKey checks the two agree.
//
// A first run produces a file, and loading it reports the missing key rather
// than an error, which is what lets the interface open and ask.
func TestInstalledDefaultLoadsAsNoKey(t *testing.T) {
	home(t)

	if err := InstallDefault(); err != nil {
		t.Fatalf("InstallDefault: %v", err)
	}

	_, found, err := Load()
	if !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Load returned %v, want ErrNoAPIKey", err)
	}
	if !found {
		t.Error("found = false, want true: the file was just installed")
	}
}

// TestPathNamesTheFile covers the accessor a message uses.
func TestPathNamesTheFile(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), `{}`, 0o600)

	if got, want := Path(), filepath.Join(h, ".orcli.json"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}
