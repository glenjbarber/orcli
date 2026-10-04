package tui

import (
	"errors"
	"io"
	"os"
)

// IsTerminal reports whether a file descriptor is a terminal.
//
// It is a termios read rather than a stat on the mode. A stat cannot answer the
// question: /dev/null is a character device and so is a pty with nothing behind
// it, so both report the same kind and only one of them is a terminal. The ioctl
// asks the descriptor itself, and it has one answer, which is whether the
// descriptor has terminal state to hand.
func IsTerminal(fd uintptr) bool { return isTerminal(fd) }

// StreamsAreTerminal reports whether both streams are terminals.
//
// Both, not either. The interface reads keys from one and draws on the other,
// and a run with only one of them a terminal is a run that cannot be drawn on or
// cannot be typed into. A redirected run is reported rather than half-worked.
func StreamsAreTerminal(stdin io.Reader, stdout io.Writer) bool {
	return fileIsTerminal(stdin) && fileIsTerminal(stdout)
}

// fileIsTerminal reports whether a stream is a terminal, and false for anything
// that is not a file.
//
// A stream this package cannot name a descriptor for is a stream it cannot ask
// the question of. A pipe and a buffer both reach that case, and both are the
// answer a redirected run needs.
func fileIsTerminal(stream any) bool {
	f, ok := stream.(*os.File)
	if !ok || f == nil {
		return false
	}
	return IsTerminal(f.Fd())
}

// ErrNoTerminal reports a run whose streams are not terminals.
//
// It is returned rather than the run continuing and drawing into a pipe, since a
// redirected interface writes escape sequences into whatever is reading and the
// reader gets noise rather than a report. The message names the reason, since a
// reader who piped this on purpose needs to be told that was the problem and not
// that the program is broken.
var ErrNoTerminal = errors.New("stdin and stdout must both be terminals")

// ErrNoSize reports a terminal that answered a size query with nothing.
//
// SizeOf returns a zero WindowSize rather than a figure it made up, since a size
// that reads as zero is not a size. A caller that drew a frame into it would be
// drawing rows nobody can see, and a reader would see a program that appeared to
// do nothing. The error is named here rather than built by each caller so the
// message says the same thing wherever the query came up empty.
var ErrNoSize = errors.New("the terminal reported no size")
