//go:build linux

package tui

import (
	"syscall"
	"unsafe"
)

// TermiosSupported reports whether this build can ask a descriptor about its
// terminal state. It is true wherever the check below is spelled.
const TermiosSupported = true

// terminalQuery is the request that reads the terminal attributes.
//
// The System V spelling is TCGETS where the BSD family carries a trailing A.
// Only that one name differs, so it is the only thing this file has to say that
// the BSD one does not.
const terminalQuery = syscall.TCGETS

// windowSizeQuery is the request that reads the terminal size.
//
// It is spelled the same on both families, so unlike TCGETS it needs no split, and
// it lives beside the termios query rather than in a file of its own because a
// reader looking for one ioctl should not have to know which file a name lives in.
const windowSizeQuery = syscall.TIOCGWINSZ

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

// windowSize is the terminal's size as the kernel reports it.
//
// Two shorts then two padding shorts. The padding is the field that is easy to
// leave out and hard to see missing: a struct eight bytes short is a read the
// kernel fills four bytes of, and the columns come back as zero, which looks like
// a terminal that cannot be asked rather than one asked wrongly.
type windowSizeRaw struct {
	Rows    uint16
	Cols    uint16
	Xpixel  uint16
	Ypixel  uint16
}

// isTerminal reports whether the descriptor is a terminal.
func isTerminal(fd uintptr) bool {
	var a attributes
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		uintptr(terminalQuery), uintptr(unsafe.Pointer(&a)))
	return errno == 0
}

// windowSize reads the terminal's rows and columns.
func windowSize(fd uintptr) (rows, cols int) {
	var ws windowSizeRaw
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		uintptr(windowSizeQuery), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0, 0
	}
	return int(ws.Rows), int(ws.Cols)
}