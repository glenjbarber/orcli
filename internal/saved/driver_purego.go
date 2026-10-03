package saved

import (
	// The driver is imported for its side effect: it registers itself under the
	// name driverName refers to.
	//
	// It is pure Go, which is the only reason it is here. make crossbuild sets
	// GOOS without a C toolchain on this host, so a driver that compiles C would
	// produce a binary that builds for every target and then fails at the first
	// query. A cross-build gate that cannot cross-build stops meaning anything,
	// which is a worse outcome than the megabytes this costs.
	//
	// The alternative was measured rather than assumed. mattn/go-sqlite3 builds on
	// this host, but no cross C toolchain is installed, so CGO_ENABLED=1 fails
	// inside runtime/cgo for every foreign target. The pure-Go driver costs about
	// twelve megabytes against three for the two tables this holds.
	_ "modernc.org/sqlite"
)
