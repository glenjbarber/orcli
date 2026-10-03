package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
)

// runIn calls run with the trust gate and the terminal check stood in for, so a
// test exercises the wiring rather than the platform.
//
// The gate is replaced because a test cannot answer a trust question, and the
// terminal check because a test has no terminal. Both are variables for exactly
// this reason, and the fact that they are is worth stating: startup logic that
// reaches the platform directly is startup logic no test can call.
func runIn(t *testing.T, trusted bool, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	stand := func() {
		gate = func(string, config.Config, io.Reader, io.Writer, io.Writer) bool { return trusted }
		streamsAreTerminal = func(io.Reader, io.Writer) bool { return true }
	}
	stand()
	t.Cleanup(stand)

	var out, errOut bytes.Buffer
	err = run(args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), err
}

// TestVersionFlagPrintsAndStops covers the ordinary flag path, and is here
// because a flag that does not stop is a flag that opens a session a script did
// not ask for.
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

// TestUnknownCommandIsRefusedByName covers the case where a script passes the
// wrong word. It is refused rather than treated as a prompt, since a script that
// got this wrong has a bug in it that an interactive session would hide.
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
// A bad flag printed twice, once as usage and once as a message, is a reader
// looking for the second one. The usage is available under --help, which is where
// a reader who wants it looks.
func TestUnknownFlagIsRefusedWithoutUsage(t *testing.T) {
	_, _, err := runIn(t, false, "--nonesuch")
	if err == nil {
		t.Fatal("run accepted an unknown flag, want a refusal")
	}
	if strings.Contains(err.Error(), "Usage") {
		t.Errorf("the refusal carries the usage: %q", err)
	}
}

// TestBadFlagIsReportedOnce pins the reason the flag package's output is
// discarded: a bad flag reported twice is worse than one reported once.
func TestBadFlagIsReportedOnce(t *testing.T) {
	_, _, err := runIn(t, false, "--dir")
	if err == nil {
		t.Fatal("run accepted a flag with no value, want a refusal")
	}
	if got := strings.Count(err.Error(), "flag needs an argument"); got != 1 {
		t.Errorf("the refusal mentions the flag %d times, want 1: %q", got, err)
	}
}

// TestSessionReportsWhatItResolved covers the report, since it is the only output
// a run produces today and it is the thing a reader will be looking at.
func TestSessionReportsWhatItResolved(t *testing.T) {
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
}

// TestSessionReportsTheTrustAnswer covers the case that matters most in the
// report: a reader who declined the directory must be able to see that they did,
// since a session with no tools and no explanation reads as a broken build.
func TestSessionReportsTheTrustAnswer(t *testing.T) {
	out, _, err := runIn(t, false, "--dir", t.TempDir())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "tools          off") {
		t.Errorf("the report does not show the tools as off:\n%s", out)
	}
}

// TestTheCredentialIsNeverPrinted is the rule that governs every line of the
// report, and it is worth a test of its own rather than being left to the reading
// of the code.
//
// The report ends up in terminal scrollback, and a credential printed there is a
// credential leaked to whoever can read that scrollback.
func TestTheCredentialIsNeverPrinted(t *testing.T) {
	const key = "sk-or-v1-the-credential"

	withHome(t, func(home string) {
		writeConfig(t, home, `{"api_key":"`+key+`"}`)

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

// TestAMissingCredentialIsNotFatal covers the decision that separates an absence
// from a fault. An interface that refuses to open leaves nothing on screen
// explaining why.
func TestAMissingCredentialIsNotFatal(t *testing.T) {
	withHome(t, func(home string) {
		writeConfig(t, home, `{"model":"some/model"}`)

		stdout, _, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run returned %v for a file with no credential, want nil", err)
		}
		if !strings.Contains(stdout, "credential     absent") {
			t.Errorf("the report does not say the credential is absent:\n%s", stdout)
		}
	})
}

// TestABadModeIsFatal covers the other side of that line. A file holding a
// credential that another account can read has already leaked, and no report
// makes that better.
func TestABadModeIsFatal(t *testing.T) {
	withHome(t, func(home string) {
		path := writeConfig(t, home, `{"api_key":"k"}`)
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
	withHome(t, func(home string) {
		writeConfig(t, home, `{"api_key":"k","approval":"sometimes"}`)

		_, _, err := runIn(t, false)
		if err == nil {
			t.Fatal("run accepted an approval mode it does not know, want a refusal")
		}
		if !strings.Contains(err.Error(), "sometimes") {
			t.Errorf("the refusal is %q, want it to quote what the file said", err)
		}
	})
}

// TestAnAbsentBootstrapDocumentIsRefusedBeforeTheTrustQuestion covers the
// ordering, which is the reason that check happens where it does.
//
// A document named by the reader that cannot be read is worth knowing about
// before being asked anything.
func TestAnAbsentBootstrapDocumentIsRefusedBeforeTheTrustQuestion(t *testing.T) {
	withHome(t, func(home string) {
		writeConfig(t, home, `{"api_key":"k"}`)

		asked := false
		gate = func(string, config.Config, io.Reader, io.Writer, io.Writer) bool {
			asked = true
			return false
		}
		streamsAreTerminal = func(io.Reader, io.Writer) bool { return true }

		missing := filepath.Join(home, "no-such-document.md")
		_, _, err := runIn2(t, []string{"--bootstrap", missing})

		if err == nil {
			t.Fatal("run accepted a bootstrap document that cannot be read")
		}
		if asked {
			t.Error("the trust question was asked before the document was checked")
		}
	})
}

// runIn2 runs with the gate and terminal check as they are, so a test can
// install its own gate first.
func runIn2(t *testing.T, args []string) (stdout, stderr string, err error) {
	t.Helper()

	var out, errOut bytes.Buffer
	err = run(args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), err
}

// TestABootstrapDirectoryIsRefused covers the shape check. A directory named as a
// document is a mistake, and reading it as one would seed a conversation with
// nothing.
func TestABootstrapDirectoryIsRefused(t *testing.T) {
	withHome(t, func(home string) {
		writeConfig(t, home, `{"api_key":"k"}`)

		_, _, err := runIn(t, false, "--bootstrap", home)
		if err == nil {
			t.Fatal("run accepted a directory as a bootstrap document")
		}
		if !strings.Contains(err.Error(), "directory") {
			t.Errorf("the refusal is %q, want it to say what is wrong with the path", err)
		}
	})
}

// TestARedirectedRunIsToldWhy covers the reason the terminal check is kept in a
// program whose interface does not exist. A reader who pipes this gets different
// behaviour and is entitled to be told which.
func TestARedirectedRunIsToldWhy(t *testing.T) {
	withHome(t, func(home string) {
		writeConfig(t, home, `{"api_key":"k"}`)

		restoreGate := gate
		restoreTerminal := streamsAreTerminal
		t.Cleanup(func() {
			gate = restoreGate
			streamsAreTerminal = restoreTerminal
		})
		gate = func(string, config.Config, io.Reader, io.Writer, io.Writer) bool { return false }
		streamsAreTerminal = func(io.Reader, io.Writer) bool { return false }

		stdout, stderr, err := runIn2(t, nil)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stderr, "terminals") {
			t.Errorf("a redirected run was not told why: %q", stderr)
		}
		if !strings.Contains(stdout, "configuration") {
			t.Errorf("a redirected run did not report the session:\n%s", stdout)
		}
	})
}

// TestTheFileDecidesWhatTheFlagsDoNot covers the precedence, which is the one
// question about flags that had not been settled and is decided here.
//
// The flag is the more recent of the two statements: a flag is something the
// reader typed now and a file is something they wrote earlier. Where both say
// the same thing there is nothing to decide.
func TestTheFileDecidesWhatTheFlagsDoNot(t *testing.T) {
	withHome(t, func(home string) {
		writeConfig(t, home, `{"api_key":"k","bell":true,"color":true,"mouse":true}`)

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

// TestAFieldWithNoValueIsADashNotNothing covers the reporting rule. A report whose
// shape shifts as values arrive is harder to read than one that holds its shape
// and says dash.
func TestAFieldWithNoValueIsADashNotNothing(t *testing.T) {
	withHome(t, func(home string) {
		writeConfig(t, home, `{"api_key":"k"}`)

		stdout, _, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !strings.Contains(stdout, "model          -") {
			t.Errorf("an unset field is not a dash:\n%s", stdout)
		}
	})
}

// withHome points the configuration search at a temporary directory for the
// duration of one test.
//
// The search order is a package variable, so this is the only way to test startup
// without reading the reader's own configuration file. A test that touched the
// real one could not run on a machine with a credential in it, and a test that
// cannot run is a test that does not.
func withHome(t *testing.T, body func(home string)) {
	t.Helper()

	home := t.TempDir()

	restoreOrder := config.SearchOrder
	config.SearchOrder = []string{filepath.Join(home, "orcli.json")}
	t.Cleanup(func() { config.SearchOrder = restoreOrder })

	// The search order names an absolute path, so the home directory itself
	// does not need to be redirected for this to work. It is left alone rather
	// than set, because a test that rewrites HOME changes the answer to
	// os.UserHomeDir for every package in the process.
	_ = home

	body(home)
}

// writeConfig writes a configuration file and returns its path.
func writeConfig(t *testing.T, home, body string) string {
	t.Helper()

	path := filepath.Join(home, "orcli.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	return path
}
