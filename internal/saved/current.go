package saved

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultSessionSuffix names the file a directory's working session is saved under.
//
// The suffix rather than a bare name is deliberate. The autosave of a directory is
// already written under the directory's own base name, so a session the reader saved
// by hand under the same name would land on the same file as the autosave and be
// overwritten by it. A distinct suffix means the two are two files, and `/restore`
// can offer one without offering the other.
const DefaultSessionSuffix = "_session"

// workingName is the name the session for a working directory is saved under.
//
// It is the base name of the directory rather than the whole path, for the reason
// autosaveName gives: a reader looking in the sessions directory finds the same file
// for the same tree, and a filename cannot hold a separator.
//
// Two directories whose base names are the same therefore share one file, which is the
// same tradeoff autosave already makes. A directory named orcli under two different
// parents is one file, and a reader who works in both overwrites one session with the
// other. The base name is what a reader recognises in a listing, so the distinction
// would cost the whole path, and a reader who needs two can name them themselves.
func workingName(dir string) (string, error) {
	base, err := autosaveName(dir)
	if err != nil {
		return "", err
	}
	return base + DefaultSessionSuffix, nil
}

// ErrNoSessionForDirectory is returned when the working directory has no session.
//
// It is a state rather than a fault, and it is distinct from ErrNoStore so a caller can
// say which. A reader who asked for a session by name and got nothing should be told
// the name was not there, and a reader who ran `/restore` in a directory with no saved
// session should be told there is none to restore. Both are absences, and the message
// a reader reads differs.
var ErrNoSessionForDirectory = errors.New("saved: no saved session for this directory to restore")

// CurrentName returns the name the session for a working directory is saved under.
//
// It is exported rather than kept inside the store because a caller showing a reader
// what will be written, or completing one, needs to name the same file the save will
// write and the restore will read. Three functions answering that question separately
// is three functions that can disagree.
func CurrentName(dir string) (string, error) { return workingName(dir) }

// Current returns the session saved for a working directory.
//
// The directory is resolved the way autosaveName resolves it, through a symlink and
// then to its base name, so a reader in a linked checkout gets the file for the
// directory they are actually in rather than for the one they typed. Two callers
// answering that question separately is how a save and a restore from one shell end up
// naming two files.
//
// A directory with no saved session is an absence rather than a fault, and is reported
// as ErrNoSessionForDirectory so a caller can tell it from a store that could not be
// read. A reader who ran `/restore` in a directory they have not worked in is not a
// fault; they are told there is nothing to restore.
func (s *Store) Current(dir string) (Session, error) {
	var session Session

	if s == nil {
		return session, ErrNoSessionForDirectory
	}

	name, err := CurrentName(dir)
	if err != nil {
		return session, err
	}

	path := filepath.Join(s.dir, name+".db")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return session, fmt.Errorf("%w: %s", ErrNoSessionForDirectory, dir)
		}
		return session, fmt.Errorf("saved: read %s: %w", path, err)
	}

	// The load is by name rather than by path, so the version check, the count
	// restoration and the turn reading stay in one place. Reaching into them here
	// would be a second reader of a file that already has one.
	return s.Load(name)
}

// SaveCurrent writes a session for a working directory, replacing the file already
// there.
//
// It is the same write Save performs, under the same name the restore reads, so a
// reader who saves twice keeps one file rather than accumulating one per turn. The
// write is whole and goes to a temporary file that is renamed over the target, so a
// reader who loses power mid-save has the old conversation rather than a truncated one.
// A database written in place can be left unreadable, and a conversation is the one
// thing here that cannot be recovered from anywhere else.
//
// The name on the session is set to the working name rather than kept, since a reader
// who saves with a name and expects the directory's file to carry it would find two
// sessions under two names where there is one.
func (s *Store) SaveCurrent(dir string, session Session) (string, error) {
	name, err := CurrentName(dir)
	if err != nil {
		return "", err
	}

	session.Name = name
	return s.Save(session)
}

// CurrentNames returns the names of the sessions that Current can restore.
//
// It is the completion source for `/restore`: a reader who has never saved a session in
// this directory should be offered nothing rather than a name that resolves to nothing.
// Only the sessions carrying the suffix are listed, so an autosave is not offered as
// something to restore and a session the reader named themselves is not hidden behind
// a rule they never asked for.
func (s *Store) CurrentNames() ([]string, error) {
	names, err := s.Names()
	if err != nil {
		return nil, err
	}

	var out []string
	for _, name := range names {
		if strings.HasSuffix(name, DefaultSessionSuffix) {
			out = append(out, name)
		}
	}
	return out, nil
}
