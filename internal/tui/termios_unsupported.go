//go:build !linux && !darwin

package tui

// TermiosSupported reports whether this build can ask a descriptor about its
// terminal state.
//
// It is false on a platform whose ioctl request this package has not spelled,
// which is the honest answer rather than a guess: a terminal check that guesses is
// a check that tells a reader their terminal is fine when the program cannot ask.
//
// The constant exists so that a caller needing a terminal rather than a guess
// checks this first, and a platform without the ioctl gets a message naming itself
// instead of a message about the terminal.
const TermiosSupported = false

// isTerminal reports whether the descriptor is a terminal.
//
// It is false here for the reason TermiosSupported is false: the question cannot be
// asked, and a false answer is the one that does not tell a reader something untrue.
func isTerminal(uintptr) bool { return false }
