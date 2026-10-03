package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// standIn writes a program into a directory and returns the path to it.
//
// The suite arranges its own programs rather than reaching for whatever the machine has,
// since what is under test is the bound around a program and not the program. A test that
// used cat or sleep would pass on a machine where they exist and skip on one where they do
// not, which is a bound that is only ever checked by accident.
//
// A shell script is written rather than a compiled one so the suite stays a suite: there is
// nothing to build and nothing to cross-compile.
func standIn(t *testing.T, name, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
	return path
}

// TestRunProgramReportsTheOutputOfAFailure covers the guarantee a bound must not break.
//
// A program that ran and failed has still told the model something. The failure is
// reported with what it wrote attached, and dropping it would make a model retry a call
// whose answer was already on the wire.
func TestRunProgramReportsTheOutputOfAFailure(t *testing.T) {
	path := standIn(t, "failer", "echo the reason\nexit 3\n")

	out, err := runProgram(path, nil, t.TempDir(), time.Minute)
	if err == nil {
		t.Fatal("a program that exited 3 succeeded, want it reported")
	}
	if !strings.Contains(string(out), "the reason") {
		t.Errorf("the output is %q, want the explanation the program gave", out)
	}
}

// TestRunProgramCarriesBothStreams covers the decision to collect them together.
//
// git explains a failure on its second stream, and a result carrying only the first is a
// failure whose explanation was thrown away.
func TestRunProgramCarriesBothStreams(t *testing.T) {
	path := standIn(t, "noisy", "echo to stdout\necho to stderr >&2\nexit 1\n")

	out, err := runProgram(path, nil, t.TempDir(), time.Minute)
	if err == nil {
		t.Fatal("a program that exited 1 succeeded, want it reported")
	}
	for _, want := range []string{"to stdout", "to stderr"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the output is %q, want it to carry %q", out, want)
		}
	}
}

// TestRunProgramReportsAFailureToStart covers the case where nothing ran at all.
//
// A path that names no program is a fault before it is anything else, and it is reported
// rather than returned as a result carrying an empty success.
func TestRunProgramReportsAFailureToStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-a-program")

	if _, err := runProgram(missing, nil, t.TempDir(), time.Minute); err == nil {
		t.Error("a program that cannot be started succeeded, want it reported")
	}
}

// TestRunProgramRefusesADeadlineOfZero covers the parameter rather than a wait on it.
//
// A deadline of zero would kill a program before it began, so a caller that passed one is
// reported rather than given a call that cannot succeed. This is the guard a new call site
// hits if it forgets the timeout.
func TestRunProgramRefusesADeadlineOfZero(t *testing.T) {
	path := standIn(t, "echoer", "echo hello\n")

	for _, timeout := range []time.Duration{0, -time.Second} {
		if _, err := runProgram(path, nil, t.TempDir(), timeout); err == nil {
			t.Errorf("a deadline of %s was accepted, want it refused", timeout)
		}
	}
}

// TestRunProgramKillsAProgramPastItsDeadline is the timeout, exercised on the short
// deadline a test can afford.
//
// A program that never finishes is a turn waiting for something that will never arrive,
// and a turn that waits is what a reader experiences as a hang rather than as a fault.
func TestRunProgramKillsAProgramPastItsDeadline(t *testing.T) {
	path := standIn(t, "sleeper", "echo starting\nsleep 30\n")

	before := time.Now()
	_, err := runProgram(path, nil, t.TempDir(), 150*time.Millisecond)
	elapsed := time.Since(before)

	if err == nil {
		t.Fatal("a program that was still running at its deadline succeeded, want it reported")
	}
	if !errors.Is(err, ErrTimedOut) {
		t.Errorf("the failure is %v, want it to carry ErrTimedOut", err)
	}

	// The wait must end at the deadline and not with the program, or the bound is not a
	// bound. The margin is loose because a loaded machine is slower than an idle one, and
	// the point is that it did not run for the sleep.
	if elapsed > 10*time.Second {
		t.Errorf("the wait took %s, want it to end near the deadline", elapsed)
	}
}

// TestRunProgramKeepsWhatAKilledProgramPrinted covers the partial answer.
//
// A program killed for running too long has usually said something on its way to that, and
// a failure carrying nothing is a failure a model has nothing to act on.
func TestRunProgramKeepsWhatAKilledProgramPrinted(t *testing.T) {
	path := standIn(t, "talker", "echo working on it\nsleep 30\n")

	out, err := runProgram(path, nil, t.TempDir(), 150*time.Millisecond)
	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("the failure is %v, want it to carry ErrTimedOut", err)
	}
	if !strings.Contains(string(out), "working on it") {
		t.Errorf("the output is %q, want what the program printed before it was killed", out)
	}
}

// TestRunProgramDoesNotWaitForAChildHoldingThePipe covers the grace window.
//
// A killed program can leave a child holding the write end of the pipe, and a read of a
// pipe nothing will close blocks until that child exits. The program here leaves one behind
// that would hold the pipe for far longer than the grace window, and the call must still
// return.
func TestRunProgramDoesNotWaitForAChildHoldingThePipe(t *testing.T) {
	path := standIn(t, "leaker", "sleep 30 &\necho done\nwait\n")

	before := time.Now()
	_, err := runProgram(path, nil, t.TempDir(), 100*time.Millisecond)
	elapsed := time.Since(before)

	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("the failure is %v, want it to carry ErrTimedOut", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("the wait took %s, want it to end near the deadline rather than at the sleep",
			elapsed)
	}
}

// TestRunProgramBoundsTheOutput is the cap, and it is checked rather than reported.
//
// A program writing a great deal is reported as over the limit, and what is reported with
// it is bounded, since keeping everything a program wrote would make the cap decoration.
func TestRunProgramBoundsTheOutput(t *testing.T) {
	path := standIn(t, "gusher", "dd if=/dev/zero bs=1024 count=2048 2>/dev/null\n")

	out, err := runProgram(path, nil, t.TempDir(), time.Minute)
	if err == nil {
		t.Fatal("a program that wrote 2 MiB succeeded, want the cap reported")
	}
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Errorf("the failure is %v, want it to carry ErrOutputTooLarge", err)
	}
	if len(out) > outputLimit {
		t.Errorf("the output is %d bytes, want it held to the %d byte limit",
			len(out), outputLimit)
	}
	if !strings.Contains(err.Error(), "over") {
		t.Errorf("the failure is %q, want it to say the output is over the limit", err)
	}
}

// TestRunProgramNamesTheLimit covers the refusal teaching the model.
//
// A bound reported as a number the reader has to convert is a bound they have to trust
// rather than check, so the message names the figure.
func TestRunProgramNamesTheLimit(t *testing.T) {
	path := standIn(t, "gusher", "dd if=/dev/zero bs=1024 count=2048 2>/dev/null\n")

	_, err := runProgram(path, nil, t.TempDir(), time.Minute)
	if err == nil {
		t.Fatal("a program that wrote 2 MiB succeeded, want the cap reported")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(outputLimit)) {
		t.Errorf("the failure is %q, want it to name the limit", err)
	}
}

// TestRunProgramKeepsTheHeadOfALargeOutput covers what survives the cap.
//
// The head of a large output is the part that explains it, so what is kept is the first
// bytes rather than the last.
//
// The marker is written before the volume rather than piped into it. A dd reading a pipe
// takes whatever the pipe offers, so `echo x | dd of=/dev/stdout bs=1024 count=2048`
// writes two bytes and never reaches the cap, on any host. The volume therefore comes
// from the same if=/dev/zero form every other over-the-cap test in this file uses.
func TestRunProgramKeepsTheHeadOfALargeOutput(t *testing.T) {
	path := standIn(t, "gusher",
		"echo the-head\ndd if=/dev/zero bs=1024 count=2048 2>/dev/null\n")

	out, err := runProgram(path, nil, t.TempDir(), time.Minute)
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("the failure is %v, want it to carry ErrOutputTooLarge", err)
	}
	if !strings.Contains(string(out), "the-head") {
		t.Error("the head of the output was not kept, want it reported with the cap")
	}
}

// TestBoundedBufferKeepsTheHeadAndCountsTheRest checks the cap where it is applied,
// which is as the output arrives rather than after the program is done.
//
// A buffer left to fill and measured afterwards bounds what is reported and not what is
// held, so a program writing a gigabyte would cost a gigabyte here and the cap would be
// decoration.
func TestBoundedBufferKeepsTheHeadAndCountsTheRest(t *testing.T) {
	var b boundedBuffer

	// Three writes that together pass the cap, so the third is the one that is cut.
	chunk := strings.Repeat("x", outputLimit/2+1)
	for i := 0; i < 3; i++ {
		if _, err := b.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	out, err := b.outcome("program", nil)
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("the outcome is %v, want it to carry ErrOutputTooLarge", err)
	}
	if len(out) != outputLimit {
		t.Errorf("the outcome kept %d bytes, want the cap of %d", len(out), outputLimit)
	}

	// The message counts what the program wrote rather than what was kept, since a
	// model told its output was cut wants to know how far past the cap it went.
	if want := len(chunk) * 3; !strings.Contains(err.Error(), strconv.Itoa(want)) {
		t.Errorf("the failure is %q, want it to name the %d bytes written", err, want)
	}
}

// TestBoundedBufferReportsEveryByteAsWritten covers the decision not to stop a program
// for writing a great deal.
//
// The cap is reported rather than cut, and cutting it by making the program see a short
// write would turn a reportable overflow into a broken program.
func TestBoundedBufferReportsEveryByteAsWritten(t *testing.T) {
	var b boundedBuffer

	chunk := make([]byte, 4096)
	if n, err := b.Write(chunk); err != nil {
		t.Fatalf("Write: %v", err)
	} else if n != len(chunk) {
		t.Errorf("Write reported %d of %d bytes, want every byte reported as written",
			n, len(chunk))
	}

	// Fill past the cap, then check that a further write is still reported whole.
	for b.buf.Len() < outputLimit {
		if _, err := b.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if n, err := b.Write(chunk); err != nil {
		t.Fatalf("Write: %v", err)
	} else if n != len(chunk) {
		t.Errorf("Write past the cap reported %d of %d bytes, want every byte reported",
			n, len(chunk))
	}
}

// TestRunProgramPassesAnOutputAtTheLimit covers the boundary, in both directions.
//
// A program that wrote exactly the limit has not been cut off, and one that wrote a byte
// more has been. Reporting the cap for the first would make every large command look like
// a fault, and passing the second would make the cap a figure rather than a bound.
func TestRunProgramPassesAnOutputAtTheLimit(t *testing.T) {
	dir := t.TempDir()

	atLimit := standIn(t, "atlimit", "dd if=/dev/zero bs=1 count=1048576 2>/dev/null\n")
	if _, err := runProgram(atLimit, nil, dir, time.Minute); err != nil {
		t.Errorf("an output of exactly the limit was reported as %v, want it passed", err)
	}

	past := standIn(t, "past", "dd if=/dev/zero bs=1 count=1048577 2>/dev/null\n")
	if _, err := runProgram(past, nil, dir, time.Minute); !errors.Is(err, ErrOutputTooLarge) {
		t.Errorf("an output of one byte past the limit was reported as %v, want the cap", err)
	}
}

// TestTheTimeoutPerToolIsSettled records the two figures and which is longer.
//
// The shell is given longer than git because a build legitimately takes longer than a
// status. A test on the relation is what holds that reason to the code, since the two are
// two constants that could be transposed without anything noticing.
func TestTheTimeoutPerToolIsSettled(t *testing.T) {
	if shellTimeout != 2*time.Minute {
		t.Errorf("the shell deadline is %s, want two minutes", shellTimeout)
	}
	if gitTimeout != 30*time.Second {
		t.Errorf("the git deadline is %s, want thirty seconds", gitTimeout)
	}
	if shellTimeout <= gitTimeout {
		t.Errorf("the shell deadline is %s and the git one is %s, want the shell longer "+
			"since a build takes longer than a status", shellTimeout, gitTimeout)
	}
	if killGrace != 500*time.Millisecond {
		t.Errorf("the grace window is %s, want 500ms", killGrace)
	}
}

// TestTheDescriptionNamesTheBounds checks the schema tells a model the truth.
//
// A model that has not been told a command will be killed at two minutes will ask for the
// whole build rather than the step it is waiting on, and a model told the cap can ask for
// the head of an output.
func TestTheDescriptionNamesTheBounds(t *testing.T) {
	shell := NewShell(tree(t)).Describe().Function.Description
	for _, want := range []string{
		shellTimeout.String(), strconv.Itoa(outputLimit), "reported rather than truncated",
	} {
		if !strings.Contains(shell, want) {
			t.Errorf("the shell description does not name %q: %s", want, shell)
		}
	}

	git := NewGit(tree(t)).Describe().Function.Description
	for _, want := range []string{gitTimeout.String(), strconv.Itoa(outputLimit)} {
		if !strings.Contains(git, want) {
			t.Errorf("the git description does not name %q: %s", want, git)
		}
	}
}
