//go:build !linux && !darwin

package tui

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// The parts of raw mode that a platform outside the set cannot spell.
//
// The pattern is the one termios_unsupported.go already sets: the package
// compiles, and the functions here report that the terminal cannot be driven
// rather than guessing. A build that fails to compile on a platform is a build a
// reader cannot use at all, and a build that claims to work there is a build that
// tells a reader their terminal is fine when the program cannot ask it anything.
//
// The requests are numbers the kernel interprets, and a number written for one
// platform is a different number on another, so there is nothing to spell here
// that would be right.

// ErrNoRawMode reports a terminal that would not enter raw mode.
//
// It is named rather than reported as a bare ioctl error, since the two mean
// different things to a reader: an ioctl error says the request was refused, while
// this says the interface cannot read keys here at all and the reader is looking
// at a program that would accept a line and never answer it.
var ErrNoRawMode = errors.New("tui: this build cannot drive the terminal, so keys cannot be read")

// rawMode reports that this platform cannot put a terminal into raw mode.
func rawMode(uintptr) (unix.Termios, error) {
	return unix.Termios{}, ErrNoRawMode
}

// restoreMode reports that there is no mode to put back, since none was changed.
func restoreMode(uintptr, unix.Termios) error { return nil }

// enterRaw reports that this platform cannot read keys.
//
// It is refused rather than left to a read that would fail later, since a caller
// that entered a loop on a terminal it cannot read would spin rather than tell
// the reader anything.
func enterRaw(uintptr) (func(), error) { return nil, ErrNoRawMode }

// escapeBracketedPaste is a no-op on a platform that cannot drive the terminal.
//
// The mode belongs to the terminal rather than to the screen, and a terminal this
// build cannot reach is one this build has no business changing.
func escapeBracketedPaste(*Screen) {}

// escapeBracketedPasteOff is a no-op for the reason escapeBracketedPaste is.
func escapeBracketedPasteOff(*Screen) {}

// flagsAt reports that this platform cannot ask a descriptor for its open flags.
func flagsAt(uintptr) (int, error) { return 0, ErrNotReadable }

// interruptSignals returns the signals this package would restore a terminal
// before.
//
// It is empty rather than a list of guesses: a signal handled on a platform that
// cannot put the terminal back is a signal swallowed with nothing restored, which
// is the failure the handler exists to prevent.
func interruptSignals() []os.Signal { return nil }
