//go:build darwin

package tui

// tcgets is the request that reads the terminal attributes.
//
// The BSD family spells it TCGETA and System V spells it TCGETS. The number is
// the same on every BSD, and it is written here rather than taken from x/sys
// because the constant is not exported under a name the toolchain can reach.
//
// It is a number rather than a symbolic constant because the request is a wire
// value the kernel interprets, and naming it by what the kernel calls it is what
// makes the split between this file and its System V counterpart legible.
const tcgets = 0x40487413
