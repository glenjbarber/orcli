//go:build darwin || linux

package tui

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// The terminal in raw mode, and the prior state to put back.
//
// Raw mode is not a convenience here. The interface reads keys itself, so the
// line discipline cannot be left to assemble a line: with ICANON on, the terminal
// waits for a newline before a read returns at all, which is a loop that cannot
// see a keystroke until the reader has decided they were finished typing, and
// with ECHO on, the terminal writes every key twice, once from the reader and
// once from the client.
//
// # What is cleared and what is not
//
// ECHO, ICANON and IEXTEN are cleared because the client draws the field and the
// terminal must not draw it as well. ISIG is cleared because a control key has to
// reach the loop rather than being turned into a signal: a reader who presses
// Ctrl-C to abandon a turn means the turn, and a terminal that raises SIGINT for
// it takes the program down instead. OPOST is cleared because the client counts
// display columns and the terminal must not translate the bytes it writes.
//
// ICRNL is deliberately left on. Clearing it would change what a carriage return
// arriving in a paste means, and the client draws rows with \r\n because that is
// what a terminal in raw mode with OPOST off expects to be given. Clearing it is
// in most raw-mode recipes on the grounds of clearing everything, and it is
// wrong here: a paste containing a carriage return would stop arriving as one.
//
// The state is saved rather than reconstructed. restoreMode puts back the exact
// termios the read returned, so a reader whose terminal had anything unusual set
// is left with their own settings rather than with this client's idea of normal.

// rawMode puts the descriptor into raw mode and returns the state to put back.
//
// The state is returned rather than held on the package, since a caller that
// wants to restore it has to be the one that decided to enter: a mode saved in a
// package variable is a mode a second session in one process overwrites, and the
// terminal restored is then the wrong one.
func rawMode(fd uintptr) (unix.Termios, error) {
	var prior unix.Termios

	current, err := unix.IoctlGetTermios(int(fd), ioctlGets)
	if err != nil {
		return prior, err
	}
	prior = *current

	raw := prior
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(int(fd), ioctlSets, &raw); err != nil {
		return prior, err
	}
	return prior, nil
}

// restoreMode puts back a termios this package read.
//
// It is a function rather than a method on a value so the caller holds the prior
// state as an ordinary value it was handed, rather than as something it has to
// ask the package to look up.
func restoreMode(fd uintptr, prior unix.Termios) error {
	return unix.IoctlSetTermios(int(fd), ioctlSets, &prior)
}

// ErrNoRawMode reports a terminal that would not enter raw mode.
//
// It is named rather than reported as the bare ioctl error, since the two mean
// different things to a reader: an ioctl error says the request was refused,
// while this says the interface cannot read keys here at all and the reader is
// looking at a program that would accept a line and never answer it.
var ErrNoRawMode = errors.New("tui: the terminal would not enter raw mode, so keys cannot be read")

// enterRaw puts the descriptor into raw mode and arranges for it to be put back.
//
// The signal handler is the part that is not optional and not this program's
// choice about its own lifetime. A process that leaves a terminal with ECHO and
// ICANON cleared leaves a shell that looks dead: the reader types and nothing
// appears. They did not cause that and they are the one who has to fix it, so
// every path out of the program restores the state, including the paths where the
// program did not choose to leave.
//
// The handler restores and then re-raises with the default disposition, so the
// signal still does what the reader's shell expects. Catching it and swallowing
// it would be a program that ignores Ctrl-C, which is worse than one that dies
// on it.
func enterRaw(fd uintptr) (func(), error) {
	prior, err := rawMode(fd)
	if err != nil {
		return nil, err
	}

	restore := func() {
		_ = restoreMode(fd, prior)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, interruptSignals()...)

	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case received := <-sig:
			restore()
			signal.Stop(sig)
			// The default disposition is restored before the re-raise, so the
			// signal kills the process the way it would have rather than arriving
			// at a handler that has already run.
			signal.Reset(received)
			if s, ok := received.(syscall.Signal); ok {
				_ = syscall.Kill(syscall.Getpid(), s)
			}
		}
	}()

	return func() {
		close(done)
		signal.Stop(sig)
		restore()
	}, nil
}

// escapeBracketedPaste turns bracketed paste on.
//
// The mode belongs to the terminal rather than to the screen, and this frame
// enters no screen: the log scrolls in the normal buffer and the reader's own
// scrollback is the transcript. So the mode is asked for when the interface
// starts and given back when it exits, and the ordering that matters is the
// exit.
func escapeBracketedPaste(screen *Screen) {
	screen.Write("\x1b[?2004h")
}

// escapeBracketedPasteOff gives the terminal its paste mode back.
//
// It is a separate function rather than the second half of the one above so the
// exit path reads as the thing it is, and so a caller that fails to enter raw
// mode does not turn a mode on that it never turned on.
//
// A terminal left believing paste is bracketed swallows a paste arriving after
// this program has gone, and that paste arrives into a shell the reader is
// waiting for.
func escapeBracketedPasteOff(screen *Screen) {
	screen.Write("\x1b[?2004l")
}
