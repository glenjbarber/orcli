//go:build linux

package tui

import (
	"syscall"
	"unsafe"
)

// TermiosSupported reports whether this build can ask a descriptor about its
// terminal state. It is true wherever the ioctl is spelled above.
const TermiosSupported = true

// terminalQuery is the request that reads the terminal attributes.
//
// The System V spelling is TCGETS where the BSD family carries a trailing A.
// Only that one name differs, so it is the only thing this file has to say that
// the BSD one does not.
const terminalQuery = syscall.TCGETS

// attributes is the terminal state the ioctl writes into.
//
// It is sized for Linux and no further: the answer to is this a terminal is
// whether the read succeeds at all, and the fields inside are the terminal's
// business rather than this client's. A raw-mode client would need them, and the
// line editor will.
type attributes struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Line   uint8
	Cc     [19]uint8
	Ispeed uint32
	Ospeed uint32
}

// isTerminal reports whether the descriptor is a terminal.
func isTerminal(fd uintptr) bool {
	var a attributes
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		uintptr(terminalQuery), uintptr(unsafe.Pointer(&a)))
	return errno == 0
}
