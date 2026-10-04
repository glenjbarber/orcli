//go:build darwin

package tui

// The two ioctl requests that read and write the terminal attributes.
//
// The number is written here rather than taken from x/sys because the library
// does not export it under a name this toolchain reaches, which is the same
// reason termios_request_darwin.go carries the one for the read. The two files
// exist so that every request number in this package is written down once.
const (
	ioctlGets = tcgets
	ioctlSets = 0x80487414
)
