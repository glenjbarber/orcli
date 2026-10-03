package saved

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// maxNameLength bounds a session name.
//
// The limit is this package's rather than the filesystem's, since the name is cleaned
// before it becomes a filename and the cleaned form is longer than the one a reader
// typed. A name past it is refused with a message that says so, rather than written and
// then unreachable by the name the reader typed.
const maxNameLength = 200

// sessionName cleans a name into something a filename can carry.
//
// Each character that cannot be in a filename is replaced rather than stripped.
// Stripping is the friendlier-looking choice and it is wrong here: two directories
// differing only in a non-ASCII character would produce one filename, and a reader who
// saved a session from each would find one of them gone.
//
// A separator is replaced rather than replaced-with-anything, but the result is
// checked: a name that was reaching for another directory is refused rather than
// quietly filed under a name that looks nothing like it. A reader who typed
// "../outside" meant to leave this directory, and a store that turned that into
// ".._outside.db" has silently answered a different question than the one asked.
//
// A literal escape is doubled before anything else is replaced, so a name carrying one
// cannot be made to look like a name that was written differently.
func sessionName(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("saved: a session needs a name")
	}

	// A name that reaches is refused before it is cleaned, since cleaning it would
	// hide the reach rather than report it.
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("saved: the name %q contains a separator; a name is one file, "+
			"not a path", name)
	}

	clean, err := escapeName(name)
	if err != nil {
		return "", err
	}
	if strings.Trim(clean, ".") == "" {
		return "", fmt.Errorf("saved: the name %q names no file", name)
	}
	return clean, nil
}

// escapeName escapes the characters a filename cannot carry, and bounds the result.
//
// It is separate from sessionName because two callers need it and one of them has no
// name of its own to reject: a name derived from a working directory carries whatever
// that directory is called, and a directory called "../elsewhere" would resolve outside
// the sessions directory without the separator check ever running.
//
// The escape is applied after the caller's own checks rather than instead of them, so
// the two rules stay distinct. A reader typing a separator is refused and told so; a
// directory holding one has its separator escaped, because the directory is not
// something a reader typed and cannot be asked about.
func escapeName(name string) (string, error) {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '%':
			// Doubled before anything else, so a name that carries one is not turned
			// into the name that would have been written without it.
			b.WriteString("%%")
		case r == 0 || r < 0x20 || r == 0x7f:
			b.WriteRune('_')
		case r < unicode.MaxASCII:
			b.WriteRune(r)
		default:
			// Replaced rather than stripped or transliterated, since either of those
			// would give two different names the same file.
			b.WriteString(fmt.Sprintf("%%%04x", r))
		}
	}

	clean := b.String()
	if len(clean) > maxNameLength {
		return "", fmt.Errorf("saved: the name is %d characters, and the limit is %d",
			len(clean), maxNameLength)
	}
	return clean, nil
}

// autosaveName is the name an autosaved session carries.
//
// It is named for the working directory rather than for the moment, so a reader
// looking at the directory finds the same file for the same tree. The moment is inside
// the file, since the metadata table carries it and a filename cannot hold a colon.
//
// The base name is escaped on the way out. It was not, and a directory whose name
// holds a character a filename cannot carry produced a name that could not be loaded
// back by the name it was listed under: the escaping happened in sessionName, after
// the file had already been named.
func autosaveName(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("saved: resolve %s: %w", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		// A directory that cannot be resolved is used as given. A symlink that has
		// been broken is a directory a reader is standing in, and refusing to name it
		// would refuse a save for a tree that exists.
		abs = resolved
	}

	base := filepath.Base(abs)
	if base == "/" || base == "." || base == "" {
		return "", errors.New("saved: the working directory has no name to save it under")
	}
	return escapeName(base)
}
