package saved

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// open opens a database at path.
//
// The driver is named here and nowhere else, so the one place that knows which driver
// this package uses is the one place a change to it would have to be made. A driver that
// compiles C is not usable here: make crossbuild sets GOOS without a C toolchain, so it
// would produce a binary that builds for every target and then fails at the first query.
//
// The connection is limited to one, since this package writes one file at a time and a
// pool of connections to a database only one goroutine is touching is a way to hold a lock
// longer than necessary.
func open(path string) (*sql.DB, error) {
	d, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("saved: open %s: %w", path, err)
	}
	d.SetMaxOpenConns(1)
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, fmt.Errorf("saved: open %s: %w", path, err)
	}
	return d, nil
}

// driverName is the driver this package opens databases with.
//
// It is a field rather than a literal so a test can point the store at a driver of its
// own, which is what makes the store testable without SQLite installed.
var driverName = "sqlite"

// absentPhrases are the wordings a driver uses for a file that is not there.
//
// They are checked rather than assumed, since a driver wording an absence differently
// would otherwise have every missing session reported as a fault, and a fault reads as
// something wrong with the reader's files rather than as a name they have not saved yet.
var absentPhrases = []string{"no such file", "does not exist", "not exist"}

// isAbsent reports whether an error from a driver means the file was not there.
//
// The system error is checked as well as the wordings, since a driver that wraps it
// rather than wording its own carries the phrase in the chain.
func isAbsent(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, phrase := range absentPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
