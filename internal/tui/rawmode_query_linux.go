//go:build linux

package tui

import "syscall"

// The two ioctl requests that read and write the terminal attributes.
//
// The System V spelling omits the trailing A the BSD family carries, so this
// file carries the pair while rawmode_query_darwin.go carries the other one. Only
// the numbers differ; everything else about a raw-mode client is the same on
// both families, which is why the rest of the change is written once.
const (
	ioctlGets = syscall.TCGETS
	ioctlSets = syscall.TCSETS
)
