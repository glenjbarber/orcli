package tools

import (
	"fmt"
	"strings"
)

// Readable is a read-only reach, used by the file tools.
//
// It is not a ticket and it is not a second working directory. It permits a path
// outside the working directory to be listed and read when that path resolves inside
// a granted root, and nothing else: no file is written there, no program runs there,
// and the working directory remains the only place either happens.
//
// The distinction is the point of it. A reader who grants a read-only reach to a
// directory of documentation has agreed to let a model read it, not to let a model act
// in it, and a mechanism that could not tell those apart would be granting the second
// while claiming the first.
type Readable struct {
	// roots are the resolved absolute paths that may be read.
	roots []string

	// writeRoot is the resolved working directory, the only place a file is written.
	writeRoot string
}

// NewReadable opens a read-only reach for the given roots.
//
// A root that cannot be resolved is reported rather than skipped. A reader who named a
// directory and got a session that quietly could not read it would have been told
// something untrue by the silence.
func NewReadable(writeRoot string, roots []string) (*Readable, error) {
	w, err := resolveRoot(writeRoot)
	if err != nil {
		return nil, err
	}

	r := &Readable{writeRoot: w}
	for _, root := range roots {
		resolved, err := resolveRoot(root)
		if err != nil {
			return nil, fmt.Errorf("tools: the readable root %s: %w", root, err)
		}
		r.roots = append(r.roots, resolved)
	}
	return r, nil
}

// Roots returns the resolved roots, for a message that names what was granted.
func (r *Readable) Roots() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.roots...)
}

// contains reports whether an absolute cleaned path is inside the working directory
// or inside one of the granted roots.
//
// The working directory is included without being granted, since it is where every call
// starts.
func (r *Readable) contains(abs string) bool {
	if within(r.writeRoot, abs) {
		return true
	}
	for _, root := range r.roots {
		if within(root, abs) {
			return true
		}
	}
	return false
}

// ReadablePath resolves a path for reading and reports whether it may be read.
//
// A relative path is taken against the working directory, since that is the base a
// model writing one means. A path inside the working directory needs no grant. A path
// outside it is refused unless it resolves inside a granted root, and the refusal names
// the roots that exist, so a reader who meant to grant something learns what to add.
func (r *Readable) ReadablePath(path string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("%w: no working directory is open", ErrOutsideRoot)
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: no path was given", ErrOutsideRoot)
	}

	abs := resolveAgainst(r.writeRoot, path)

	if !r.contains(abs) {
		return "", fmt.Errorf("%w: %s is not readable; the readable directories are %s",
			ErrOutsideRoot, path, describeRoots(r.roots))
	}
	return abs, nil
}

// WritablePath resolves a path for writing.
//
// There is no grant that widens this. The working directory is the only place a file is
// written, so a reader who wanted a second writable directory has not got one and is
// not going to get one from this package.
func (r *Readable) WritablePath(path string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("%w: no working directory is open", ErrOutsideRoot)
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: no path was given", ErrOutsideRoot)
	}

	abs := resolveAgainst(r.writeRoot, path)

	// A path inside a readable root but outside the working directory is refused by
	// name rather than as an escape, since a reader who granted a read-only reach
	// would reasonably wonder why a write there did not work. The distinction is
	// worth the branch: the reader learns which rule they met.
	if !within(r.writeRoot, abs) {
		for _, root := range r.roots {
			if within(root, abs) {
				return "", fmt.Errorf("%w: %s is inside a readable directory, and those are read only",
					ErrOutsideRoot, path)
			}
		}
		return "", fmt.Errorf("%w: %s", ErrOutsideRoot, path)
	}
	return abs, nil
}

// describeRoots renders the granted roots for a refusal.
func describeRoots(roots []string) string {
	if len(roots) == 0 {
		return "none"
	}
	return strings.Join(roots, ", ")
}
