package saved

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// driverName is the driver this package opens databases with.
//
// It is a field rather than a literal so a test can point the store at a driver of its
// own, which is what makes the store testable without SQLite installed. The default is
// the pure-Go one, which is the whole reason a cross build can happen at all.
var driverName = "sqlite"

// DriverName returns the driver this package opens databases with.
//
// It is exported so the caller can say which driver is in use in a message, since a
// reader who is told a session store is unavailable is better served by being told why.
func DriverName() string { return driverName }

// Available reports whether the driver this package would use is registered.
//
// A session that cannot open a store says so once at startup rather than failing on the
// first save, since a reader who is told a session store is unavailable can still hold a
// conversation without one.
func Available() bool {
	for _, driver := range sql.Drivers() {
		if driver == driverName {
			return true
		}
	}
	return false
}

// open opens an existing database at path.
//
// A path that does not exist is reported rather than created. The pure-Go driver creates
// the file on open, which would turn a reader naming a session they have not saved yet
// into an empty file appearing beside their real ones: Names would then list it, and the
// next save of that name would overwrite whatever it was meant to be. So the check is
// made here, before the driver is given a chance to be helpful.
func open(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, errAbsent{path: path}
	} else if err != nil {
		return nil, fmt.Errorf("saved: read %s: %w", path, err)
	}
	return connect(path)
}

// create opens a database at path, creating it if it is absent.
//
// It is the form a save uses, since a save is what brings a session file into being.
// Nothing reads through it: a read that found nothing is a session the reader has not
// saved yet, and creating it there would be the wrong answer to a question about the
// past.
func create(path string) (*sql.DB, error) {
	return connect(path)
}

// connect opens a database without asking whether it is there.
func connect(path string) (*sql.DB, error) {
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

// errAbsent is the error for a file that is not there.
//
// It is a type rather than a wrapped system error so that the absence survives the trip
// through the driver's own error, which is where a wrapped fs.ErrNotExist would be
// reported as something the caller cannot recognize.
type errAbsent struct{ path string }

// Error implements error.
func (e errAbsent) Error() string { return "no such file: " + e.path }

// Is reports the system error as its own, so errors.Is(err, fs.ErrNotExist) answers true
// for a caller that expects that rather than for this package's wording.
func (e errAbsent) Is(target error) bool { return target == fs.ErrNotExist }

// absentPhrases are the wordings a driver uses for a file that is not there.
//
// They are checked in addition to the check above, since a driver may report an absence
// for a path the caller believes exists: a directory entry that vanished, or a mount that
// went away. Both are a session the reader has not got rather than a fault in their
// files.
var absentPhrases = []string{"no such file", "does not exist", "not exist"}

// isAbsent reports whether an error from a driver means the file was not there.
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
