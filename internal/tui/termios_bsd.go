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

// windowSize reads the terminal's rows and columns.
//
// The request is TIOCGWINSZ and it is spelled the same on both families, so it is
// written once here rather than in a build-tagged pair. What differs is the struct
// it fills: the BSD layout is two shorts followed by two padding shorts, and a
// struct that disagreed with the kernel's would be a short read reporting a
// terminal narrower than it is.
func windowSize(fd uintptr) (rows, cols int) {
	ws, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0
	}
	return int(ws.Row), int(ws.Col)
}