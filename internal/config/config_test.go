package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// home is a temporary home directory, with the environment pointed inside it.
//
// HOME is set rather than whatever the reader has, so a test cannot read the
// real configuration of whoever is running it.
func home(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
	return dir
}

// writeConfigAt writes a body into the configuration path of a temporary home,
// and returns the path.
func writeConfigAt(t *testing.T, h, body string) string {
	t.Helper()

	path := filepath.Join(h, ".orcli.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), FileMode); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(path, FileMode); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	return path
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

// readConfig returns the bytes of a configuration file.
func readConfig(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return data
}

// relax sets the mode of a file, so a test can make it permissive on purpose.
func relax(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()

	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
}

// TestLoadReadsTheKey covers the ordinary path.
func TestLoadReadsTheKey(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"sk-or-v1-abc","model":"some/model"}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.APIKey, "sk-or-v1-abc"; got != want {
		t.Errorf("APIKey = %q, want %q", got, want)
	}
	if got, want := cfg.Model, "some/model"; got != want {
		t.Errorf("Model = %q, want %q", got, want)
	}
}

// TestLoadIgnoresTheEnvironmentKey is the rule that matters most here.
//
// A key in the environment does not shadow the file. The class of failure this
// removes is a correct file overridden by a stale value elsewhere, and a reader
// sent to edit their shell profile instead of the file that is read.
func TestLoadIgnoresTheEnvironmentKey(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"sk-or-v1-from-file"}`)

	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-from-environment")

	cfg, err := Load()
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
// session without one.
func TestLoadIgnoresTheEnvironmentKeyWhenTheFileHasNone(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"model":"some/model"}`)

	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-from-environment")

	if _, err := Load(); !errors.Is(err, ErrNoAPIKey) {
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
	writeConfigAt(t, h,
		`{"model":"some/model","bell":true,"OPENROUTER_TRUSTED":["/tmp/one"]}`)

	cfg, err := Load()
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("Load returned %v, want ErrNoAPIKey", err)
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

// TestLoadReportsNoFileAsNotFound covers the first run, which is a state.
func TestLoadReportsNoFileAsNotFound(t *testing.T) {
	home(t)

	if _, err := Load(); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load returned %v, want ErrNotFound", err)
	}
}

// TestLoadRefusesAPermissiveMode covers the hard failure.
//
// A permissive mode leaks the credential silently, so it is refused at startup
// with the mode named, rather than warned about and used.
func TestLoadRefusesAPermissiveMode(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), `{"api_key":"sk-or-v1-abc"}`, 0o644)

	_, err := Load()
	if !errors.Is(err, ErrBadMode) {
		t.Fatalf("Load returned %v, want ErrBadMode", err)
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
				`{"api_key":"sk-or-v1-abc"}`, mode)

			if _, err := Load(); !errors.Is(err, ErrBadMode) {
				t.Errorf("Load accepted mode %04o, want it refused", mode)
			}
		})
	}
}

// TestLoadRefusesAMalformedFile covers a fault rather than a state.
func TestLoadRefusesAMalformedFile(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{not json`)

	if _, err := Load(); err == nil {
		t.Error("Load returned nil, want an error for unreadable content")
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

	_, err := Load()
	if err == nil {
		t.Fatal("Load returned nil, want an error for a directory at the path")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("failure is %v, want a directory reported rather than an absence", err)
	}
}

// TestLoadPrefersTheFirstFile covers the search order, and the absence of a
// merge.
//
// The first match wins. Merging two files that both claim to be the
// configuration is a way to have no configuration.
func TestLoadPrefersTheFirstFile(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), `{"api_key":"sk-or-v1-first"}`, 0o600)
	writeConfig(t, filepath.Join(h, ".config", "orcli", "orcli.json"),
		`{"api_key":"sk-or-v1-second"}`, 0o600)

	cfg, err := Load()
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
	writeConfig(t, filepath.Join(h, ".config", "orcli", "orcli.json"),
		`{"api_key":"sk-or-v1-xdg"}`, 0o600)

	cfg, err := Load()
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
	if got := info.Mode().Perm(); got != FileMode {
		t.Errorf("mode is %04o, want %04o", got, FileMode)
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
	body := `{"api_key":"sk-or-v1-kept","somethingNew":{"a":1}}`
	writeConfig(t, path, body, 0o600)

	if err := InstallDefault(); err != nil {
		t.Fatalf("InstallDefault: %v", err)
	}
	if got := string(readConfig(t, path)); got != body {
		t.Errorf("the file is now %s, want it left exactly as it was", got)
	}
}

// TestInstallDefaultCarriesNoKey covers why the default key is empty.
//
// A file carrying a placeholder would suppress the first-time setup that tells
// a reader what to do.
func TestInstallDefaultCarriesNoKey(t *testing.T) {
	h := home(t)
	writeConfig(t, filepath.Join(h, ".orcli.json"), "{}", 0o600)

	if err := InstallDefault(); err != nil {
		t.Fatalf("InstallDefault: %v", err)
	}

	data := readConfig(t, filepath.Join(h, ".orcli.json"))
	if strings.Contains(string(data), "sk-or") {
		t.Errorf("the installed default carries a key: %s", data)
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
	if _, err := Load(); !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Load returned %v, want ErrNoAPIKey", err)
	}
}

// TestPathNamesTheFile covers the accessor a message uses.
func TestPathNamesTheFile(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{}`)

	if got, want := Path(), filepath.Join(h, ".orcli.json"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}
