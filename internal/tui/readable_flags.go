//go:build darwin || linux

package tui

import "golang.org/x/sys/unix"

// flagsAt reads a descriptor's open flags.
//
// It is a function rather than a remembered value because a caller of readWithin
// may hand it a descriptor this package did not put into a mode of its own, and
// the only way to put it back the way it was found is to have asked.
func flagsAt(fd uintptr) (int, error) {
	return unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
}
