//go:build darwin

package tui

import "golang.org/x/sys/unix"

// TermiosSupported reports whether this build can ask a descriptor about its
// terminal state. It is true wherever the check below is spelled.
const TermiosSupported = true

// isTerminal reports whether the descriptor is a terminal.
//
// The request comes from golang.org/x/sys/unix rather than from syscall, because
// syscall does not carry the BSD spelling of TCGETA. That makes x/sys a direct
// dependency rather than an indirect one, and it is worth saying why: this is the
// price of a terminal check that compiles on the platform the reader is on, and
// the alternative is a build that fails there.
func isTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), tcgets)
	return err == nil
}
