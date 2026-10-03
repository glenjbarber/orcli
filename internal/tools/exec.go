package tools

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// lookPath finds a program on PATH.
//
// It is a field rather than a direct call so a test can point the shell tool at a
// directory of stand-in programs instead of at whatever happens to be installed on
// the machine running the suite. The shipped behavior is exec.LookPath, which is
// what a reader's machine has.
var lookPath = exec.LookPath

// ErrOutputTooLarge is returned when a program wrote more than the cap.
//
// It is a distinct error so a caller can tell a program that wrote a great deal from one
// that failed, since the two are reported differently: this one carries what was read
// before the cap, because the head of a large output is the part that explains it.
var ErrOutputTooLarge = errors.New("tools: the output is over the limit")

// ErrTimedOut is returned when a program was still running at the deadline.
//
// It is reported rather than left as a wait, since a program that never finishes is a turn
// waiting for something that will never arrive, and a turn that waits is what a reader
// experiences as a hang rather than as a fault.
var ErrTimedOut = errors.New("tools: the program did not finish in time")

// outputLimit is the most a program may write and still have all of it read.
//
// The same figure bounds a file read, so a model told an output is over the limit cannot
// tell from the message whether the read or the command produced it.
const outputLimit = 1 << 20

// killGrace is how long the wait is given to end on its own after the deadline.
//
// A killed program can leave a child holding the write end of the pipe, and a read of a
// pipe nothing will close blocks until that child exits. So the program is killed at the
// deadline and the wait is given this long to finish by itself. A child that ends inside
// the window costs nothing, and a child that does not is abandoned rather than waited on.
const killGrace = 500 * time.Millisecond

// shellTimeout is how long one shell command may run.
//
// It is longer than gitTimeout because a build legitimately takes longer than a status.
const shellTimeout = 2 * time.Minute

// gitTimeout is how long one git command may run.
const gitTimeout = 30 * time.Second

// boundedBuffer holds at most outputLimit bytes of output and counts what it did not keep.
//
// The bound is applied as the output arrives rather than after the program has finished. A
// buffer left to fill and measured afterwards bounds what is reported and not what is held,
// so a program writing a gigabyte would cost a gigabyte here and the cap would be
// decoration.
//
// A write past the limit is still reported to the program as written in full. The program is
// not stopped for writing a great deal, because the decision recorded in the design is that
// the cap is reported rather than cut, and killing the writer would turn a reportable
// overflow into a truncated answer.
type boundedBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	dropped int
}

// Write keeps what fits and counts the rest, reporting every byte as written.
func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if room := outputLimit - b.buf.Len(); room > 0 {
		if len(p) <= room {
			return b.buf.Write(p)
		}
		if _, err := b.buf.Write(p[:room]); err != nil {
			return 0, err
		}
		b.dropped += len(p) - room
		return len(p), nil
	}
	b.dropped += len(p)
	return len(p), nil
}

// outcome is what the collected output and the failure to report with it are.
//
// The bytes kept are returned even when the call failed, since a program that ran and
// failed has still told the model something and throwing it away would make a model retry
// a call whose answer was already on the wire.
func (b *boundedBuffer) outcome(path string, cause error) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := b.buf.Bytes()
	if b.dropped > 0 {
		wrote := len(out) + b.dropped
		return out, fmt.Errorf("%w: %s wrote %d bytes, and this client reads at most %d; "+
			"what is reported here is its first %d", ErrOutputTooLarge,
			filepath.Base(path), wrote, outputLimit, outputLimit)
	}
	return out, cause
}

// runProgram runs a resolved program in dir with an argument array, under a deadline.
//
// exec.Command is given the program as a resolved path and the arguments as separate
// entries, so nothing inside an argument is ever interpreted: a pipe in an argument is a
// character in a string and not an operator. The environment is set explicitly rather
// than inherited, since a subprocess that inherits the reader's environment inherits
// whatever secrets happen to be in it.
//
// Three bounds live here rather than at each call site, because every program this package
// runs arrives through this one function: the deadline, so a program that never finishes
// does not hold a turn for ever; the output, so a program that writes a great deal cannot
// fill the memory of the client; and the wait after the deadline, so killing a program
// that left a child behind does not turn into waiting on that child.
//
// Both streams go into one buffer. The two are separate pipes from the point of view of the
// program and one from the point of view of the reader, and a program that explains its own
// failure writes to the second one, so a result carrying only the first is a failure whose
// explanation was thrown away.
func runProgram(path string, args []string, dir string, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("tools: no deadline is set for %s", filepath.Base(path))
	}

	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = environment()

	// The buffer is written by a goroutine in the standard library for each of the two
	// streams, so it carries a lock. One lock and one buffer for both streams is cheaper
	// than a mutex per stream and loses nothing, since the two never need to be told apart.
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// The wait runs on its own goroutine so the deadline below can be a timer rather than
	// a second process, and the channel is buffered so that goroutine is not left holding
	// a send nobody will read when the deadline wins.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return out.outcome(path, err)
	case <-time.After(timeout):
		// The program is killed rather than left running. A program this tool started is a
		// program this tool is answerable for, and one that outlives the call would be
		// writing to the tree with no turn waiting for it.
		_ = cmd.Process.Kill()
	}

	// The child is given killGrace to let go of the pipes before they are closed from the
	// outside. This wait is bounded too, since it is itself a select against a timer, so
	// a child holding a pipe open cannot turn a kill into a second hang.
	timer := time.NewTimer(killGrace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}

	// What the program printed on its way to being killed is kept, since a program killed
	// for running too long has usually said something, and a failure carrying nothing is a
	// failure a model has nothing to act on. An overflow is reported in preference to the
	// timeout when both happened, because it is the more specific of the two.
	return out.outcome(path, fmt.Errorf("%w: %s was still running after %s and was killed",
		ErrTimedOut, filepath.Base(path), timeout))
}
