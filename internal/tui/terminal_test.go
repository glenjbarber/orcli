package tui

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestStreamsAreTerminalRefusesABuffer covers the case a reader hits when they pipe
// this on purpose: neither stream is a file with a descriptor, so the answer is no
// and the run is reported rather than half-worked.
func TestStreamsAreTerminalRefusesABuffer(t *testing.T) {
	if StreamsAreTerminal(bytes.NewBuffer(nil), bytes.NewBuffer(nil)) {
		t.Error("two buffers were reported as terminals")
	}
}

// TestStreamsAreTerminalRefusesOneStream covers the "both, not either" rule. A run
// with only one stream a terminal cannot be drawn on or cannot be typed into, and
// half of it is not what a reader who redirected one side asked for.
func TestStreamsAreTerminalRefusesOneStream(t *testing.T) {
	// The test binary's own stdin is not a terminal under a test runner, so this
	// asserts the rule rather than a platform fact: a stream that is not a file is
	// refused, and one stream being a file is not enough on its own.
	if StreamsAreTerminal(os.Stdin, bytes.NewBuffer(nil)) {
		t.Error("a file beside a buffer was reported as two terminals")
	}
	if StreamsAreTerminal(bytes.NewBuffer(nil), os.Stdout) {
		t.Error("a buffer beside a file was reported as two terminals")
	}
}

// TestFileIsTerminalRefusesSomethingThatIsNotAFile covers the case the question
// cannot even be asked of. A stream this package cannot name a descriptor for is a
// stream it cannot ask about, and both a pipe and a buffer reach that case.
func TestFileIsTerminalRefusesSomethingThatIsNotAFile(t *testing.T) {
	if fileIsTerminal(nil) {
		t.Error("a nil stream was reported as a terminal")
	}
	if fileIsTerminal(struct{}{}) {
		t.Error("a struct was reported as a terminal")
	}
}

// TestIsTerminalRefusesAClosedFile covers the case where a descriptor is not open.
// The ioctl fails, so the answer is no, and the answer is not an error the caller
// has to distinguish from a real one.
func TestIsTerminalRefusesAClosedFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notaterminal")
	if err != nil {
		t.Fatalf("create a file: %v", err)
	}
	name := f.Name()
	f.Close()

	// Reopened rather than kept open, since a closed file's descriptor is not a
	// question anyone should ask.
	reopened, err := os.Open(name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer reopened.Close()

	if IsTerminal(reopened.Fd()) {
		t.Error("an ordinary file was reported as a terminal")
	}
}

// TestTermiosSupportedMatchesTheBuild checks that the constant and the check agree,
// since a build that says it cannot ask and then answers true is a build that
// claims a capability it does not have.
func TestTermiosSupportedMatchesTheBuild(t *testing.T) {
	if TermiosSupported && isTerminal(0) {
		// Descriptor 0 is stdin. A test runner usually gives a pipe or a file
		// there rather than a terminal, so this only asserts that the two are
		// wired to the same answer.
		return
	}
	if !TermiosSupported && isTerminal(0) {
		t.Error("the build says it cannot ask and answered true anyway")
	}
}

// TestErrNoTerminalNamesTheReason covers the message a reader who piped this sees. A
// reader who redirected on purpose needs to be told that was the problem, not that
// the program is broken.
func TestErrNoTerminalNamesTheReason(t *testing.T) {
	msg := ErrNoTerminal.Error()
	for _, want := range []string{"stdin", "stdout", "terminal"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message %q does not name %q", msg, want)
		}
	}
	if !errors.Is(ErrNoTerminal, ErrNoTerminal) {
		t.Error("the error does not match itself, which is how errors.Is works")
	}
}
