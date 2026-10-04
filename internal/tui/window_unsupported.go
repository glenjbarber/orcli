//go:build !darwin && !linux

package tui

// This platform has no resize signal this package names, so the flag is never
// raised and the paint asks the terminal every frame regardless.
//
// That is the point of the split between the signal and the size. A frame that
// depended on being told to re-read would be correct on a platform that names
// SIGWINCH and wrong on one that does not, which is the opposite of what a reader
// would want from a terminal that is telling them nothing about its own size.
//
// So the same two functions are here as on the pair that has the signal, and both
// are honest: the watch holds nothing and the flag is never set.

// watchWindow returns a function that stops a watch that is not running.
//
// It exists so a caller starts and stops the watch on every platform with one
// call site, rather than the loop carrying a build tag of its own to ask whether
// the platform has a signal.
func watchWindow() func() { return func() {} }

// resized reports whether a resize was seen, and here there never is one.
//
// The flag is a plain false rather than an atomic, since nothing writes it on this
// platform and a read of a constant is not a race.
func resized() bool { return false }
