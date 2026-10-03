package tools

import (
	"path/filepath"
)

// resolveAgainst returns path in absolute, symlink-resolved form, with a relative
// path taken against base.
//
// Every comparison in this package is between two paths that have been through here,
// and the reason is worth stating once: on macOS a temporary directory arrives as
// /var/folders/... and is a symlink to /private/var/folders/.... Comparing a path the
// caller wrote against a root that was resolved makes the two different strings naming
// the same directory, so a legitimate path reads as an escape and the check refuses
// everything under it.
//
// The symlinks are followed as far as they exist. EvalSymlinks fails on a path that does
// not exist, and the ordinary case for a path being checked is one being created: rm is
// asked to remove what a build left behind, and a model asks for a file it has not
// written yet. So the existing prefix is followed and the rest is left as written.
func resolveAgainst(base, path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(base, abs)
	}
	abs = filepath.Clean(abs)

	if linked, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(linked)
	}
	return filepath.Clean(resolveExisting(abs))
}

// resolveExisting returns the path with every component that exists followed to
// where it points.
//
// The prefix is found by walking up until a component resolves, so a symlink anywhere
// in the part of the path that exists is followed rather than missed. A link in the
// middle of a path is exactly what a cleaned string comparison cannot see, and this is
// where that case is caught.
func resolveExisting(path string) string {
	rest := ""
	current := filepath.Clean(path)

	for {
		if linked, err := filepath.EvalSymlinks(current); err == nil {
			if rest == "" {
				return linked
			}
			return filepath.Join(linked, rest)
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Nothing in the path exists, and there is nowhere left to walk.
			return filepath.Clean(path)
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}
