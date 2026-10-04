package tui

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// Reading a descriptor with a bound, without a deadline on the file.
//
// # Why not a deadline
//
// Two approaches were tried and both fail on a standard stream.
//
// `SetReadDeadline` returns ErrNoDeadline, because Go hands a program's standard
// streams to it through os.NewFile, which leaves the descriptor blocking and
// outside the runtime poller. The deadline is not honoured rather than refused,
// and a read that cannot be bounded is a loop that cannot notice it should stop.
//
// `select(2)` would need a different descriptor-set spelling on every BSD, and
// `poll(2)` on Darwin ignores its timeout and waits for ever when nothing is
// ready, which is the exact case the bound exists for: a terminal with nothing
// typed on it.
//
// # What is left
//
// A read that cannot block, plus a short sleep between attempts. The descriptor is
// put back the way it was found, because the mode of a descriptor is not this
// client's to keep: a program that left stdin non-blocking on exit would leave
// every process that inherits it reading in a way it did not ask for.

// readPollStep is how long a read attempt waits before trying again.
//
// Two milliseconds is short enough that a keypress is not visibly delayed and
// long enough that a loop waiting at a terminal is not spinning a core. The bound
// is the sum of these, so this is also the resolution of every timeout in the key
// reader: a bound of zero returns without waiting and a bound of one step is one
// attempt.
const readPollStep = 2 * time.Millisecond

// ErrNotReadable reports a descriptor that cannot be read with a bound.
//
// It is named rather than reported as the bare syscall error, since a reader who
// sees this is being told the interface cannot read keys at all, which is a
// different thing from a read that failed once.
var ErrNotReadable = errors.New("tui: this descriptor cannot be read without blocking")

// readWithin reads from fd into p, giving up after the bound.
//
// It reports how much it read. A read that returns nothing and no error is the
// ordinary case at a terminal with nothing typed on it, and it is not a failure:
// the key reader loops on it rather than treating it as one.
func readWithin(fd uintptr, p []byte, bound time.Duration) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	// The mode is read before it is changed, so a descriptor this package does not
	// own goes back the way it was found rather than the way this package
	// believes a terminal should be left.
	prior, err := flagsAt(fd)
	if err != nil {
		return 0, err
	}
	if err := unix.SetNonblock(int(fd), true); err != nil {
		return 0, err
	}
	defer func() {
		_ = unix.SetNonblock(int(fd), prior&unix.O_NONBLOCK != 0)
	}()

	deadline := time.Now().Add(bound)
	for {
		n, err := unix.Read(int(fd), p)
		switch {
		case n > 0:
			return n, nil
		case err == nil, errors.Is(err, unix.EAGAIN), errors.Is(err, unix.EINTR):
			// Nothing yet, or a signal arrived. Both mean try again rather than
			// report: a signal is how this program is asked to stop, and the reader
			// is told through the context rather than through a failed read.
			if !time.Now().Before(deadline) {
				return 0, nil
			}
		default:
			return 0, err
		}
		time.Sleep(readPollStep)
	}
}
