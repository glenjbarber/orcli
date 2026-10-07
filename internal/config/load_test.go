package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts a configuration file on disk at the given mode and returns its
// path.
//
// The mode is written explicitly in every case rather than left to the process
// umask, because a test that passed or failed according to the umask of the
// machine running it would not be a test at all.
func write(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	// WriteFile applies the umask, so the mode is set again explicitly.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("set the mode: %v", err)
	}
	return path
}

// TestParseReadsEveryNamedField checks that a field this client names is read
// as the file wrote it.
func TestParseReadsEveryNamedField(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json", `{
		"api_key": "sk-or-v1-a-key",
		"model": "stealth/space-bunny-alpha",
		"provider": "openrouter.ai",
		"mouse": true,
		"bell": true,
		"color": true,
		"verbosity": 3,
		"approval": "allow",
		"ORCLI_TRUSTED": ["/home/a/project", "/home/a/other"]
	}`, FileMode)

	cfg, err := Parse([]byte(`{
		"api_key": "sk-or-v1-a-key",
		"model": "stealth/space-bunny-alpha",
		"provider": "openrouter.ai",
		"mouse": true,
		"bell": true,
		"color": true,
		"verbosity": 3,
		"approval": "allow",
		"ORCLI_TRUSTED": ["/home/a/project", "/home/a/other"]
	}`), path)
	if err != nil {
		t.Fatalf("Parse returned %v, want nil", err)
	}

	if cfg.APIKey != "sk-or-v1-a-key" {
		t.Errorf("APIKey is %q", cfg.APIKey)
	}
	if cfg.Model != "stealth/space-bunny-alpha" {
		t.Errorf("Model is %q", cfg.Model)
	}
	if !cfg.Mouse || !cfg.Bell || !cfg.Color {
		t.Errorf("the toggles are %v/%v/%v, want all true", cfg.Mouse, cfg.Bell, cfg.Color)
	}
	if cfg.Verbosity != 3 {
		t.Errorf("Verbosity is %d, want 3", cfg.Verbosity)
	}
	if cfg.Approval != "allow" {
		t.Errorf("Approval is %q", cfg.Approval)
	}
	if len(cfg.Trusted) != 2 {
		t.Errorf("Trusted is %v, want two entries", cfg.Trusted)
	}
}

// TestDefaultSetsThePaneColors covers the defaults a reader who has set neither
// pane colour gets: the same blue and green internal/tui's own role table
// already uses, rather than two unfamiliar colours.
func TestDefaultSetsThePaneColors(t *testing.T) {
	cfg := Default()

	if cfg.PaneActiveColor != DefaultPaneActiveColor {
		t.Errorf("PaneActiveColor is %q, want %q", cfg.PaneActiveColor, DefaultPaneActiveColor)
	}
	if cfg.PaneDoneColor != DefaultPaneDoneColor {
		t.Errorf("PaneDoneColor is %q, want %q", cfg.PaneDoneColor, DefaultPaneDoneColor)
	}
}

// TestParseReadsThePaneColors covers the two pane colours as a reader's own file
// would carry them, apart from the plain Color on/off toggle.
func TestParseReadsThePaneColors(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json", `{
		"api_key": "sk-or-v1-a-key",
		"color": true,
		"pane_active_color": "#112233",
		"pane_done_color": "#445566"
	}`, FileMode)

	cfg, err := Parse([]byte(`{
		"api_key": "sk-or-v1-a-key",
		"color": true,
		"pane_active_color": "#112233",
		"pane_done_color": "#445566"
	}`), path)
	if err != nil {
		t.Fatalf("Parse returned %v, want nil", err)
	}

	if cfg.PaneActiveColor != "#112233" {
		t.Errorf("PaneActiveColor is %q, want #112233", cfg.PaneActiveColor)
	}
	if cfg.PaneDoneColor != "#445566" {
		t.Errorf("PaneDoneColor is %q, want #445566", cfg.PaneDoneColor)
	}
}

// TestParseRefusesAMalformedPaneColor covers the report a reader who mistyped
// one gets: the field named, rather than the colour silently dropped.
func TestParseRefusesAMalformedPaneColor(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json", `{
		"api_key": "sk-or-v1-a-key",
		"pane_active_color": "blue"
	}`, FileMode)

	_, err := Parse([]byte(`{
		"api_key": "sk-or-v1-a-key",
		"pane_active_color": "blue"
	}`), path)
	if err == nil {
		t.Fatal("Parse returned nil, want a refusal naming pane_active_color")
	}
	if !strings.Contains(err.Error(), "pane_active_color") {
		t.Errorf("Parse error is %q, want it to name pane_active_color", err)
	}
}

// TestParseIgnoresUnknownKeys covers a file written by a newer version.
//
// A reader whose configuration gained a key they do not understand must not be
// locked out of it. Refusing the file would mean the only way to recover is to
// delete the key, which is a worse outcome than not reading it.
func TestParseIgnoresUnknownKeys(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json",
		`{"api_key":"k","a_field_from_the_future":true,"another":[1,2]}`, FileMode)

	cfg, err := Parse([]byte(
		`{"api_key":"k","a_field_from_the_future":true,"another":[1,2]}`), path)
	if err != nil {
		t.Fatalf("Parse returned %v, want nil: an unknown key is not a fault", err)
	}
	if cfg.APIKey != "k" {
		t.Errorf("APIKey is %q, want the credential still read", cfg.APIKey)
	}
}

// TestParseTreatsNullAsAbsent checks that a null preference is the default
// rather than an error.
//
// A reader who wrote `"model": null` meant no model, not a broken file.
func TestParseTreatsNullAsAbsent(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json",
		`{"api_key":"k","model":null,"color":null,"ORCLI_TRUSTED":null}`, FileMode)

	cfg, err := Parse([]byte(
		`{"api_key":"k","model":null,"color":null,"ORCLI_TRUSTED":null}`), path)
	if err != nil {
		t.Fatalf("Parse returned %v, want nil", err)
	}
	if cfg.Model != Default().Model {
		t.Errorf("Model is %q, want the default", cfg.Model)
	}
	if cfg.Color {
		t.Error("Color is true, want the default of false")
	}
	if len(cfg.Trusted) != 0 {
		t.Errorf("Trusted is %v, want it empty", cfg.Trusted)
	}
}

// TestParseReadsQuotedVerbosity covers a number a reader typed in quotes.
//
// A preference is not a wire field, and JSON does not allow a quoted number, so
// refusing it would punish a reader for writing `"3"`.
func TestParseReadsQuotedVerbosity(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json",
		`{"api_key":"k","verbosity":"4"}`, FileMode)

	cfg, err := Parse([]byte(`{"api_key":"k","verbosity":"4"}`), path)
	if err != nil {
		t.Fatalf("Parse returned %v, want nil", err)
	}
	if cfg.Verbosity != 4 {
		t.Errorf("Verbosity is %d, want 4", cfg.Verbosity)
	}
}

// TestParseReportsAMissingKeyNotAFault is the distinction the design leans on.
//
// No credential is a state the interface reports in the pane. It is not fatal,
// and the configuration that was read comes back with the error so the caller
// can open anyway.
func TestParseReportsAMissingKeyNotAFault(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json",
		`{"model":"m","color":true}`, FileMode)

	cfg, err := Parse([]byte(`{"model":"m","color":true}`), path)
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("Parse returned %v, want ErrNoAPIKey", err)
	}
	if cfg.Model != "m" {
		t.Errorf("Model is %q, want the configuration returned alongside the error", cfg.Model)
	}
	if !cfg.Color {
		t.Error("Color is false, want the preferences read even without a credential")
	}
}

// TestParseTreatsAnEmptyKeyAsAbsent covers a key written as an empty string.
//
// The backend answers an empty credential exactly as it answers an invalid one,
// so reporting this as present would open an interface that cannot work.
func TestParseTreatsAnEmptyKeyAsAbsent(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json", `{"api_key":""}`, FileMode)

	if _, err := Parse([]byte(`{"api_key":""}`), path); !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Parse returned %v, want ErrNoAPIKey for an empty credential", err)
	}
}

// TestParseRefusesAPermissiveMode is the fatal case, and the reason this
// package reads a mode at all.
//
// The file holds a credential. By the time a permissive mode is reported it has
// already leaked, so the answer is to stop rather than to warn.
func TestParseRefusesAPermissiveMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o666, 0o640, 0o604, 0o777} {
		path := write(t, t.TempDir(), "orcli.json", `{"api_key":"k"}`, mode)

		if _, err := Parse([]byte(`{"api_key":"k"}`), path); !errors.Is(err, ErrBadMode) {
			t.Errorf("mode %#o returned %v, want ErrBadMode", mode, err)
		}
	}
}

// TestParseAcceptsMode0600 covers the mode that is meant to work, so the mode
// check cannot pass by refusing everything.
func TestParseAcceptsMode0600(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json", `{"api_key":"k"}`, FileMode)

	if _, err := Parse([]byte(`{"api_key":"k"}`), path); err != nil {
		t.Errorf("Parse returned %v, want nil for mode 0600", err)
	}
}

// TestParseRefusesAMalformedFile covers a file that does not parse.
//
// This is a fault rather than a state: a reader who wrote it has something to
// fix, and continuing with defaults would show a configuration they did not
// write.
func TestParseRefusesAMalformedFile(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json", `{"api_key":`, FileMode)

	_, err := Parse([]byte(`{"api_key":`), path)
	if err == nil {
		t.Fatal("Parse returned nil, want an error for a malformed file")
	}
	if errors.Is(err, ErrNoAPIKey) || errors.Is(err, ErrBadMode) {
		t.Errorf("Parse returned %v, want a parse failure rather than one of the others", err)
	}
}

// TestParseRefusesATopLevelThatIsNotAnObject covers the trap.
//
// An array or a bare number parses as JSON, so a reader would otherwise be told
// the file is valid while nothing in it could be read.
func TestParseRefusesATopLevelThatIsNotAnObject(t *testing.T) {
	for _, body := range []string{`["a","b"]`, `42`, `"a string"`, `true`, `null`} {
		path := write(t, t.TempDir(), "orcli.json", body, FileMode)

		if _, err := Parse([]byte(body), path); !errors.Is(err, ErrNotAnObject) {
			t.Errorf("body %q returned %v, want ErrNotAnObject", body, err)
		}
	}
}

// TestParseReportsAPathThatIsADirectory covers the case the design names
// explicitly.
//
// A directory at the configuration path is an error rather than an absence. An
// absence is first-run setup; a directory is something a reader put there.
func TestParseReportsAPathThatIsADirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orcli.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}

	_, err := Parse([]byte(`{}`), path)
	if err == nil {
		t.Fatal("Parse returned nil, want an error for a directory")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("Parse returned %v, want it to say the path is a directory", err)
	}
}

// TestParseRefusesAWronglyTypedField covers a field of the wrong shape.
//
// A color of "yes" is a reader mistake worth reporting rather than a preference
// worth guessing at, since guessing means the interface behaves in a way the
// reader did not ask for and cannot see why.
func TestParseRefusesAWronglyTypedField(t *testing.T) {
	for _, body := range []string{
		`{"api_key":"k","color":"yes"}`,
		`{"api_key":"k","verbosity":"not a number"}`,
		`{"api_key":"k","ORCLI_TRUSTED":"a string"}`,
		`{"api_key":"k","ORCLI_TRUSTED":[1,2]}`,
		`{"api_key":123}`,
	} {
		path := write(t, t.TempDir(), "orcli.json", body, FileMode)

		if _, err := Parse([]byte(body), path); err == nil {
			t.Errorf("body %q was accepted, want an error for a wrong type", body)
		}
	}
}

// TestLoadReportsNotFoundWhenThereIsNoFile covers the state that is not a
// fault.
//
// No file anywhere on the search order is first-run setup, and it is reported
// as its own error so the caller can tell it from a file that is there and
// wrong.
func TestLoadReportsNotFoundWhenThereIsNoFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "empty"))

	_, err := Load()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Load returned %v, want ErrNotFound", err)
	}
}

// TestLoadReadsTheFirstMatch covers the search order and the absence of a
// merge.
//
// The first path that exists wins, and a second file further along the order is
// not consulted. A merge would produce a configuration that parses and is
// wrong, which is the outcome this design refuses.
func TestLoadReadsTheFirstMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	write(t, home, ".orcli.json", `{"api_key":"from-the-first"}`, FileMode)
	if err := os.MkdirAll(filepath.Join(home, ".config", "orcli"), 0o700); err != nil {
		t.Fatalf("make the second directory: %v", err)
	}
	write(t, filepath.Join(home, ".config", "orcli"), "orcli.json",
		`{"api_key":"from-the-second"}`, FileMode)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned %v, want nil", err)
	}
	if cfg.APIKey != "from-the-first" {
		t.Errorf("APIKey is %q, want the first match and no merge", cfg.APIKey)
	}
}

// TestLoadFallsThroughToTheSecondPath covers the search order in the other
// direction, so the first test cannot pass by only ever reading one file.
func TestLoadFallsThroughToTheSecondPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".config", "orcli"), 0o700); err != nil {
		t.Fatalf("make the second directory: %v", err)
	}
	write(t, filepath.Join(home, ".config", "orcli"), "orcli.json",
		`{"api_key":"from-the-second"}`, FileMode)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned %v, want nil", err)
	}
	if cfg.APIKey != "from-the-second" {
		t.Errorf("APIKey is %q, want the second path read", cfg.APIKey)
	}
}

// TestLoadIgnoresTheEnvironmentForTheCredential is the rule that removes a
// class of failure.
//
// The credential is never read from the process environment, even when it is
// set. A correct file shadowed by a stale value elsewhere is the failure this
// removes, and an environment variable is exactly that.
func TestLoadIgnoresTheEnvironmentForTheCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENROUTER_API_KEY", "from-the-environment")
	t.Setenv("ORCLIC_API_KEY", "from-the-environment")

	write(t, home, ".orcli.json", `{"api_key":"from-the-file"}`, FileMode)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned %v, want nil", err)
	}
	if cfg.APIKey != "from-the-file" {
		t.Errorf("APIKey is %q, want the file and not the environment", cfg.APIKey)
	}
}

// TestDefaultIsUsableWithoutAFile covers what a session runs with when there
// is nothing on disk.
//
// Colour is off and the approval mode is ask, which are the two defaults that
// decide whether a reader is asked before a program runs.
func TestDefaultIsUsableWithoutAFile(t *testing.T) {
	cfg := Default()

	if cfg.Color {
		t.Error("Color is true, want it off by default")
	}
	if cfg.Approval != "ask" {
		t.Errorf("Approval is %q, want ask", cfg.Approval)
	}
	if cfg.Provider != "openrouter.ai" {
		t.Errorf("Provider is %q, want the OpenRouter endpoint", cfg.Provider)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey is %q, want it empty", cfg.APIKey)
	}
	if cfg.BreakIntervalMinutes != 22 {
		t.Errorf("BreakIntervalMinutes is %d, want 22", cfg.BreakIntervalMinutes)
	}
	if cfg.BreakBell {
		t.Error("BreakBell is true, want it off by default")
	}
}

// TestParseReadsBreakFields checks that the screen-break preferences are read
// the way every other preference is.
func TestParseReadsBreakFields(t *testing.T) {
	path := write(t, t.TempDir(), "orcli.json",
		`{"api_key":"k","break_interval_minutes":10,"break_bell":true}`, FileMode)

	cfg, err := Parse([]byte(
		`{"api_key":"k","break_interval_minutes":10,"break_bell":true}`), path)
	if err != nil {
		t.Fatalf("Parse returned %v, want nil", err)
	}
	if cfg.BreakIntervalMinutes != 10 {
		t.Errorf("BreakIntervalMinutes is %d, want 10", cfg.BreakIntervalMinutes)
	}
	if !cfg.BreakBell {
		t.Error("BreakBell is false, want true")
	}
}

// TestParseRejectsNonPositiveBreakInterval checks that a file naming a
// break_interval_minutes of zero or less is reported by name rather than
// silently given the default instead.
func TestParseRejectsNonPositiveBreakInterval(t *testing.T) {
	for _, minutes := range []string{"0", "-5"} {
		body := `{"api_key":"k","break_interval_minutes":` + minutes + `}`
		path := write(t, t.TempDir(), "orcli.json", body, FileMode)

		_, err := Parse([]byte(body), path)
		if err == nil {
			t.Errorf("Parse with break_interval_minutes %s returned nil, want an error", minutes)
		}
	}
}
