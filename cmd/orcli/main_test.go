package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// runIn calls run with the trust gate stood in for, so a test exercises the wiring
// rather than the trust question.
//
// The gate is a variable for exactly this reason, and the fact that it is one is
// worth stating: startup logic that reaches the terminal for its answer directly is
// startup logic no test can call.
//
// The terminal check is not stood in for here. It is a real termios read in
// internal/tui, and the streams a test passes are buffers, which is the case a
// reader reaches by piping the program and is therefore the case worth covering.
//
// The context is a background one that is never cancelled, since a test wants the run
// to finish on its own rather than be stopped by a deadline the test did not ask for.
func runIn(t *testing.T, trusted bool, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	stand := func() {
		gate = func(string, config.Config, io.Reader, io.Writer, io.Writer) bool { return trusted }
	}
	stand()
	t.Cleanup(stand)

	var out, errOut bytes.Buffer
	err = run(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), err
}

// TestVersionFlagPrintsAndStops covers the ordinary flag path, and is here because
// a flag that does not stop is a flag that opens a session a script did not ask
// for.
func TestVersionFlagPrintsAndStops(t *testing.T) {
	out, _, err := runIn(t, false, "--version")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, version) {
		t.Errorf("output %q does not carry the version %q", out, version)
	}
	if strings.Contains(out, "configuration") {
		t.Error("--version printed a session report: the flag did not stop")
	}
}

// TestVersionSubcommandIsTheSameAnswer checks that the flag and the subcommand
// agree. Two spellings of one question that can disagree is a script that works
// against one of them and not the other.
func TestVersionSubcommandIsTheSameAnswer(t *testing.T) {
	flagged, _, err := runIn(t, false, "--version")
	if err != nil {
		t.Fatalf("run --version: %v", err)
	}
	subbed, _, err := runIn(t, false, "version")
	if err != nil {
		t.Fatalf("run version: %v", err)
	}
	if flagged != subbed {
		t.Errorf("--version printed %q and version printed %q", flagged, subbed)
	}
}

// TestHelpFlagAndSubcommandAgree covers the same for the usage, for the same
// reason.
func TestHelpFlagAndSubcommandAgree(t *testing.T) {
	flagged, _, err := runIn(t, false, "--help")
	if err != nil {
		t.Fatalf("run --help: %v", err)
	}
	subbed, _, err := runIn(t, false, "help")
	if err != nil {
		t.Fatalf("run help: %v", err)
	}
	if flagged != subbed {
		t.Error("--help and help printed different usage")
	}
	if !strings.Contains(flagged, "--bootstrap") {
		t.Error("the usage does not list --bootstrap")
	}
}

// TestTheUsageSaysWhatTheInterfaceDoes covers the usage a reader reads before the
// interface has ever opened, since on a redirect it is the only thing telling them
// what to do.
func TestTheUsageSaysWhatTheInterfaceDoes(t *testing.T) {
	out, _, err := runIn(t, false, "--help")
	if err != nil {
		t.Fatalf("run --help: %v", err)
	}

	for _, want := range []string{"Type a question", "/cloudflare", "/quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("the usage does not carry %q:\n%s", want, out)
		}
	}
}

// TestUnknownCommandIsRefusedByName covers the case where a script passes the wrong
// word. It is refused rather than treated as a prompt, since a script that got this
// wrong has a bug in it that an interactive session would hide.
func TestUnknownCommandIsRefusedByName(t *testing.T) {
	_, _, err := runIn(t, false, "conversation")
	if err == nil {
		t.Fatal("run accepted an unknown command, want a refusal")
	}
	if !strings.Contains(err.Error(), "conversation") {
		t.Errorf("the refusal is %q, want it to name the command", err)
	}
}

// TestUnknownFlagIsRefusedWithoutUsage covers the decision to discard the flag
// package's own output.
//
// A bad flag printed twice, once as usage and once as a message, is a reader looking
// for the second one. The usage is available under --help, which is where a reader
// who wants it looks.
func TestUnknownFlagIsRefusedWithoutUsage(t *testing.T) {
	_, _, err := runIn(t, false, "--nonesuch")
	if err == nil {
		t.Fatal("run accepted an unknown flag, want a refusal")
	}
	if strings.Contains(err.Error(), "Usage") {
		t.Errorf("the refusal carries the usage: %q", err)
	}
}

// TestBadFlagIsReportedOnce pins the reason the flag package's output is discarded: a
// bad flag reported twice is worse than one reported once.
func TestBadFlagIsReportedOnce(t *testing.T) {
	_, _, err := runIn(t, false, "--dir")
	if err == nil {
		t.Fatal("run accepted a flag with no value, want a refusal")
	}
	if got := strings.Count(err.Error(), "flag needs an argument"); got != 1 {
		t.Errorf("the refusal mentions the flag %d times, want 1: %q", got, err)
	}
}

// TestSessionReportsWhatItResolved covers the report, since on a redirected run it is
// the only output there is and it is the thing a reader will be looking at.
func TestSessionReportsWhatItResolved(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		out, _, err := runIn(t, true, "--dir", t.TempDir())
		if err != nil {
			t.Fatalf("run: %v", err)
		}

		for _, want := range []string{"configuration", "credential", "approval", "tools", "directory"} {
			if !strings.Contains(out, want) {
				t.Errorf("the report does not carry %q:\n%s", want, out)
			}
		}
		if !strings.Contains(out, "tools          on") {
			t.Errorf("the report does not show the tools as on:\n%s", out)
		}
	})
}

// TestSessionReportsTheTrustAnswer covers the case that matters most in the report: a
// reader who declined the directory must be able to see that they did, since a
// session with no tools and no explanation reads as a broken build.
func TestSessionReportsTheTrustAnswer(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		out, _, err := runIn(t, false, "--dir", t.TempDir())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(out, "tools          off") {
			t.Errorf("the report does not show the tools as off:\n%s", out)
		}
	})
}

// TestTheCredentialIsNeverPrinted is the rule that governs every line of the report,
// and it is worth a test of its own rather than being left to the reading of the
// code.
//
// The report ends up in terminal scrollback, and a credential printed there is a
// credential leaked to whoever can read that scrollback. The key is written into a
// real configuration file rather than handed to the report directly, so the test
// covers the path a reader's own file takes.
func TestTheCredentialIsNeverPrinted(t *testing.T) {
	const key = "sk-or-v1-the-credential"

	withHome(t, func() {
		writeConfig(t, `{"api_key":"`+key+`"}`)

		stdout, stderr, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}

		for name, stream := range map[string]string{"stdout": stdout, "stderr": stderr} {
			if strings.Contains(stream, key) {
				t.Errorf("the credential was printed on %s: %q", name, stream)
			}
		}
		if !strings.Contains(stdout, "credential     present") {
			t.Errorf("the report does not say the credential is present:\n%s", stdout)
		}
	})
}

// TestAMissingCredentialIsNotFatal covers the decision that separates an absence from
// a fault. An interface that refuses to open leaves nothing on screen explaining why.
func TestAMissingCredentialIsNotFatal(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"model":"some/model"}`)

		stdout, _, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run returned %v for a file with no credential, want nil", err)
		}
		if !strings.Contains(stdout, "credential     absent") {
			t.Errorf("the report does not say the credential is absent:\n%s", stdout)
		}
	})
}

// TestABadModeIsFatal covers the other side of that line. A file holding a credential
// that another account can read has already leaked, and no report makes that better.
func TestABadModeIsFatal(t *testing.T) {
	withHome(t, func() {
		path := writeConfig(t, `{"api_key":"k"}`)
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod: %v", err)
		}

		_, _, err := runIn(t, false)
		if err == nil {
			t.Fatal("run started with a credential file at mode 0644, want a refusal")
		}
		if !errors.Is(err, config.ErrBadMode) {
			t.Errorf("the refusal is %v, want it to carry ErrBadMode", err)
		}
	})
}

// TestAnUnknownApprovalModeIsFatalByName covers the case where the reader wrote
// something this client does not understand. Substituting a default would do
// something other than what they wrote, and silently.
func TestAnUnknownApprovalModeIsFatalByName(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k","approval":"sometimes"}`)

		_, _, err := runIn(t, false)
		if err == nil {
			t.Fatal("run accepted an approval mode it does not know, want a refusal")
		}
		if !strings.Contains(err.Error(), "sometimes") {
			t.Errorf("the refusal is %q, want it to quote what the file said", err)
		}
	})
}

// TestAnAbsentBootstrapDocumentIsRefusedBeforeTheTrustQuestion covers the ordering,
// which is the reason that check happens where it does.
//
// A document named by the reader that cannot be read is worth knowing about before
// being asked anything, and a trust question put to a reader who is about to be told
// the program cannot start is a question asked for nothing.
func TestAnAbsentBootstrapDocumentIsRefusedBeforeTheTrustQuestion(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		asked := false
		restore := gate
		t.Cleanup(func() { gate = restore })
		gate = func(string, config.Config, io.Reader, io.Writer, io.Writer) bool {
			asked = true
			return false
		}

		_, _, err := runIn(t, false, "--bootstrap", filepath.Join(t.TempDir(), "no-such.md"))
		if err == nil {
			t.Fatal("run accepted a bootstrap document that cannot be read")
		}
		if asked {
			t.Error("the trust question was asked before the document was checked")
		}
	})
}

// TestABootstrapDirectoryIsRefused covers the shape check. A directory named as a
// document is a mistake, and reading it as one would seed a conversation with
// nothing.
func TestABootstrapDirectoryIsRefused(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		_, _, err := runIn(t, false, "--bootstrap", t.TempDir())
		if err == nil {
			t.Fatal("run accepted a directory as a bootstrap document")
		}
		if !strings.Contains(err.Error(), "directory") {
			t.Errorf("the refusal is %q, want it to say what is wrong with the path", err)
		}
	})
}

// TestARedirectedRunIsToldWhy covers the reason the terminal check is kept. A reader
// who pipes this gets different behaviour and is entitled to be told which.
//
// The streams here are buffers, so the check answers on its own rather than being
// stood in for, which is the case a reader reaches by typing orcli into a pipe.
func TestARedirectedRunIsToldWhy(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		_, stderr, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stderr, "terminals") {
			t.Errorf("a redirected run was not told why: %q", stderr)
		}
	})
}

// TestAQuotedBooleanIsRefusedByName covers the case that has bitten a reader more
// than once. The report prints bell as the word on, a reader edits the file by hand,
// and a string where a boolean belongs stops startup with a message about a value
// rather than about what to write.
func TestAQuotedBooleanIsRefusedByName(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k","bell":"on"}`)

		_, _, err := runIn(t, false)
		if err == nil {
			t.Fatal("run accepted a quoted boolean")
		}
		if !strings.Contains(err.Error(), "bell") {
			t.Errorf("the refusal is %q, want it to name the member", err)
		}
	})
}

// TestTheFileDecidesWhatTheFlagsDoNot covers the precedence, which is the one question
// about flags that had not been settled and is decided here.
//
// Where a flag and a file say the same thing there is nothing to decide, so the test
// is that a file written earlier is honoured when a flag says nothing.
func TestTheFileDecidesWhatTheFlagsDoNot(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k","bell":true,"color":true,"mouse":true}`)

		stdout, _, err := runIn(t, false, "--dir", t.TempDir())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		for _, want := range []string{"bell           on", "colour         on", "mouse          on"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("the report does not carry %q:\n%s", want, stdout)
			}
		}
	})
}

// TestSessionReportsTheConfiguredBreakInterval covers the default, read from the
// configuration file's own default rather than from a flag.
func TestSessionReportsTheConfiguredBreakInterval(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		stdout, _, err := runIn(t, false, "--dir", t.TempDir())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stdout, "break          every 22m, bell off") {
			t.Errorf("the report does not carry the default break interval:\n%s", stdout)
		}
	})
}

// TestBreakIntervalFlagOverridesTheFile covers the one exception to
// TestTheFileDecidesWhatTheFlagsDoNot: --break-interval is a figure, not a
// toggle, and a reader who passes one means it literally rather than meaning
// "at least as much as the file already says."
func TestBreakIntervalFlagOverridesTheFile(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k","break_interval_minutes":10}`)

		stdout, _, err := runIn(t, false, "--dir", t.TempDir(), "--break-interval", "5", "--break-bell")
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stdout, "break          every 5m, bell on") {
			t.Errorf("the report does not show the flag's figure:\n%s", stdout)
		}
	})
}

// TestANegativeBreakIntervalIsRefusedByName covers the same rule the
// configuration file's own break_interval_minutes is held to: a negative
// figure is reported rather than silently floored.
func TestANegativeBreakIntervalIsRefusedByName(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		_, _, err := runIn(t, false, "--dir", t.TempDir(), "--break-interval", "-5")
		if err == nil {
			t.Fatal("run returned nil, want a refusal")
		}
		if !strings.Contains(err.Error(), "break-interval") {
			t.Errorf("the refusal is %q, want it to name --break-interval", err)
		}
	})
}

// TestSessionReportsTheConfiguredPaneColors covers the defaults, read from the
// configuration file's own defaults rather than from a flag: the pane-state
// colours are a second, distinct pair from the plain Color toggle, and the
// report carries both.
func TestSessionReportsTheConfiguredPaneColors(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		stdout, _, err := runIn(t, false, "--dir", t.TempDir())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stdout, "pane active    "+config.DefaultPaneActiveColor) {
			t.Errorf("the report does not carry the default active colour:\n%s", stdout)
		}
		if !strings.Contains(stdout, "pane done      "+config.DefaultPaneDoneColor) {
			t.Errorf("the report does not carry the default done colour:\n%s", stdout)
		}
	})
}

// TestPaneColorFlagsOverrideTheFile covers the same override rule the other
// flags follow: a reader who passes --pane-active-color or --pane-done-color
// gets that colour instead of whatever the file named.
func TestPaneColorFlagsOverrideTheFile(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k","pane_active_color":"#000000","pane_done_color":"#000000"}`)

		stdout, _, err := runIn(t, false, "--dir", t.TempDir(),
			"--pane-active-color", "#112233", "--pane-done-color", "#445566")
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stdout, "pane active    #112233") {
			t.Errorf("the report does not show the flag's active colour:\n%s", stdout)
		}
		if !strings.Contains(stdout, "pane done      #445566") {
			t.Errorf("the report does not show the flag's done colour:\n%s", stdout)
		}
	})
}

// TestAMalformedPaneColorFlagIsRefusedByName covers the same rule the
// configuration file's own pane_active_color and pane_done_color are held to:
// a value that is not #rrggbb is reported by name rather than silently
// dropped.
func TestAMalformedPaneColorFlagIsRefusedByName(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		_, _, err := runIn(t, false, "--dir", t.TempDir(), "--pane-active-color", "blue")
		if err == nil {
			t.Fatal("run returned nil, want a refusal")
		}
		if !strings.Contains(err.Error(), "--pane-active-color") {
			t.Errorf("the refusal is %q, want it to name --pane-active-color", err)
		}
	})
}

// TestAFieldWithNoValueIsADashNotNothing covers the reporting rule. A report whose
// shape shifts as values arrive is harder to read than one that holds its shape and
// says dash.
func TestAFieldWithNoValueIsADashNotNothing(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		stdout, _, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stdout, "model          -") {
			t.Errorf("an unset field is not a dash:\n%s", stdout)
		}
	})
}

// TestOrNoneKeepsTheShape covers the helper behind that, since a report reading a dash
// in one place and nothing in another is a report whose columns do not line up.
func TestOrNoneKeepsTheShape(t *testing.T) {
	if got := orNone(""); got != "-" {
		t.Errorf("orNone gave %q for an empty string, want a dash", got)
	}
	if got := orNone("value"); got != "value" {
		t.Errorf("orNone gave %q, want it unchanged", got)
	}
}

// TestEnabledRendersAsAWord covers a report that reads as English rather than as a
// column of booleans, which is what a reader scanning a startup report is reading.
func TestEnabledRendersAsAWord(t *testing.T) {
	if got := enabled(true); got != "on" {
		t.Errorf("enabled(true) = %q, want on", got)
	}
	if got := enabled(false); got != "off" {
		t.Errorf("enabled(false) = %q, want off", got)
	}
}

// TestErrNoTerminalNamesTheReason covers the message a reader who pipes this sees. A
// reader who redirected on purpose needs to be told that was the problem and not that
// the program is broken.
func TestErrNoTerminalNamesTheReason(t *testing.T) {
	msg := tui.ErrNoTerminal.Error()
	for _, want := range []string{"stdin", "stdout", "terminal"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message %q does not name %q", msg, want)
		}
	}
}

// withHome points the configuration search at a temporary directory for the duration of
// one test.
//
// The search order is a package variable, so this is the only way to test startup
// without reading the reader's own configuration file. A test that touched the real
// one could not run on a machine with a credential in it, and a test that cannot run
// is a test that does not.
func withHome(t *testing.T, body func()) {
	t.Helper()

	home := t.TempDir()

	restoreOrder := config.SearchOrder
	config.SearchOrder = []string{filepath.Join(home, "orcli.json")}
	t.Cleanup(func() { config.SearchOrder = restoreOrder })

	body()
}

// writeConfig writes a configuration file at 0600 in the temporary home and returns
// its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := config.SearchOrder[0]
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	return path
}
