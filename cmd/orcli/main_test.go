package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// TestVersionSubcommand covers the one command that cannot fail, since a script
// asking what this build is should not have to read a configuration file to be
// answered.
func TestVersionSubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := subcommand("version", nil, &out); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(out.String(), "orcli") {
		t.Errorf("version printed %q, want it to name the program", out.String())
	}
}

// TestHelpSubcommand covers the usage message, and that it names the commands a
// script can actually run rather than a longer list of intentions.
func TestHelpSubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := subcommand("help", nil, &out); err != nil {
		t.Fatalf("help: %v", err)
	}

	msg := out.String()
	for _, want := range []string{"orcli version", "orcli help", "--bootstrap"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the usage does not name %q", want)
		}
	}
}

// TestUnknownSubcommandIsRefusedByName covers the case where a script passed the
// wrong word. Running an interactive session would hide a bug in the caller behind a
// session that never finishes.
func TestUnknownSubcommandIsRefusedByName(t *testing.T) {
	err := subcommand("chat", nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("an unknown command was accepted, want a refusal")
	}
	if !strings.Contains(err.Error(), "chat") {
		t.Errorf("the refusal is %q, want it to name what was refused", err)
	}
}

// TestCredentialNeverPrintsTheKey covers the rule that matters most in a startup
// report. Every line of it ends up in terminal scrollback, and the credential is
// the one string that must never be in one.
func TestCredentialNeverPrintsTheKey(t *testing.T) {
	if got := credential("sk-or-v1-secret"); got != "present" {
		t.Errorf("credential printed %q, want it to report presence only", got)
	}
	if got := credential(""); got != "absent" {
		t.Errorf("credential printed %q for no key, want absent", got)
	}

	var out bytes.Buffer
	printSession(&out, session{
		Config:     config.Config{APIKey: "sk-or-v1-secret"},
		Approval:   config.ApprovalAsk,
		WorkingDir: "/tmp",
	})

	if strings.Contains(out.String(), "sk-or-v1-secret") {
		t.Errorf("the session report carries the credential:\n%s", out.String())
	}
}

// TestOrNoneKeepsTheShape covers the rule that a field with no value is a dash
// rather than nothing, so the report does not shift as values arrive.
func TestOrNoneKeepsTheShape(t *testing.T) {
	if got := orNone(""); got != "-" {
		t.Errorf("orNone gave %q for an empty string, want a dash", got)
	}
	if got := orNone("value"); got != "value" {
		t.Errorf("orNone gave %q, want it unchanged", got)
	}
}

// TestReadableRefusesADirectory covers the bootstrap document check, since a
// directory is not a document and opening one succeeding would seed a session with
// nothing.
func TestReadableRefusesADirectory(t *testing.T) {
	if err := readable(t.TempDir()); err == nil {
		t.Error("a directory was accepted as a document")
	}
}

// TestReadableRefusesAMissingFile covers the other half, since a bootstrap document
// that cannot be read is worth knowing about before any credential work is
// attempted.
func TestReadableRefusesAMissingFile(t *testing.T) {
	err := readable(t.TempDir() + "/nothing-here")
	if err == nil {
		t.Fatal("a missing document was accepted")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refusal is %v, want it to carry the absence", err)
	}
}

// TestAFileIsNotATerminal covers the property the check rests on, and the case that
// shows the fix was worth making: an ordinary file is a character device on some
// systems and a regular file on others, and a stat cannot tell a terminal from
// either.
func TestAFileIsNotATerminal(t *testing.T) {
	if !tui.TermiosSupported {
		t.Skip("this build cannot ask a descriptor about its terminal state")
	}

	f, err := os.CreateTemp(t.TempDir(), "notaterminal")
	if err != nil {
		t.Fatalf("create a file: %v", err)
	}
	defer f.Close()

	if tui.IsTerminal(f.Fd()) {
		t.Error("an ordinary file was reported as a terminal")
	}
}

// TestStreamsAreTerminalRefusesBuffers covers the case the old stub could not
// distinguish from the truth. It returned false always, which is the right answer
// here and the wrong answer at a terminal, so nothing could catch it.
func TestStreamsAreTerminalRefusesBuffers(t *testing.T) {
	if tui.StreamsAreTerminal(bytes.NewBuffer(nil), bytes.NewBuffer(nil)) {
		t.Error("two buffers were reported as terminals")
	}
}

// TestErrNoTerminalNamesTheReason covers the message a reader who pipes this sees. A
// reader who redirected on purpose needs to be told that was the problem and not
// that the program is broken.
func TestErrNoTerminalNamesTheReason(t *testing.T) {
	msg := tui.ErrNoTerminal.Error()
	for _, want := range []string{"stdin", "stdout", "terminal"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message %q does not name %q", msg, want)
		}
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
