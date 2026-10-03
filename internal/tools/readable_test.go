package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadablePermitsAGrantedRoot is the ordinary case for the reach.
func TestReadablePermitsAGrantedRoot(t *testing.T) {
	work := tree(t)
	outside := tree(t)

	r, err := NewReadable(work, []string{outside})
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	resolved, err := r.ReadablePath(filepath.Join(outside, "one.txt"))
	if err != nil {
		t.Fatalf("reading inside a granted root: %v", err)
	}
	if !strings.HasSuffix(resolved, "one.txt") {
		t.Errorf("resolved to %q, want the file inside the granted root", resolved)
	}
}

// TestReadableNeedsNoGrantForTheWorkingDirectory covers the ordinary case for the
// other half.
func TestReadableNeedsNoGrantForTheWorkingDirectory(t *testing.T) {
	work := tree(t)

	r, err := NewReadable(work, nil)
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	if _, err := r.ReadablePath("one.txt"); err != nil {
		t.Errorf("reading inside the working directory: %v", err)
	}
}

// TestReadableRefusesAnUngrantedDirectory is the bound the reach exists to draw.
func TestReadableRefusesAnUngrantedDirectory(t *testing.T) {
	work := tree(t)
	other := tree(t)
	granted := tree(t)

	r, err := NewReadable(work, []string{granted})
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	_, err = r.ReadablePath(filepath.Join(other, "one.txt"))
	if err == nil {
		t.Fatal("an ungranted directory was readable, want it refused")
	}

	// The refusal names the roots, so a reader who meant to grant something learns
	// what is granted.
	if !strings.Contains(err.Error(), filepath.Base(granted)) &&
		!strings.Contains(err.Error(), "readable directories") {
		t.Errorf("the failure is %q, want it to name what is readable", err)
	}
}

// TestReadableRefusesToWalkOutOfAGrantedRoot covers the escape from a grant.
func TestReadableRefusesToWalkOutOfAGrantedRoot(t *testing.T) {
	work := tree(t)
	secret := tree(t)
	granted := t.TempDir()

	// A directory inside the granted root that links out of it.
	if err := os.Symlink(filepath.Join(secret, "one.txt"), filepath.Join(granted, "escape")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	r, err := NewReadable(work, []string{granted})
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	if _, err := r.ReadablePath(filepath.Join(granted, "escape")); err == nil {
		t.Error("a symlink out of a granted root was readable, want it refused")
	}
}

// TestReadableIsReadOnly is the property the grant is named for.
//
// A path inside a readable root may be read and may not be written, and the
// refusal says why rather than merely saying no.
func TestReadableIsReadOnly(t *testing.T) {
	work := tree(t)
	granted := tree(t)

	r, err := NewReadable(work, []string{granted})
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	_, err = r.WritablePath(filepath.Join(granted, "new.txt"))
	if err == nil {
		t.Fatal("a write inside a readable root succeeded, want it refused")
	}
	if !strings.Contains(err.Error(), "read only") {
		t.Errorf("the failure is %q, want it to say the directory is read only", err)
	}
}

// TestWritableStaysInTheWorkingDirectory covers the writer side.
func TestWritableStaysInTheWorkingDirectory(t *testing.T) {
	work := tree(t)
	granted := tree(t)

	r, err := NewReadable(work, []string{granted})
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	if _, err := r.WritablePath("sub/new.txt"); err != nil {
		t.Errorf("writing inside the working directory: %v", err)
	}

	for _, path := range []string{"../outside.txt", "../../etc/passwd", "/tmp/elsewhere"} {
		if _, err := r.WritablePath(path); err == nil {
			t.Errorf("writing to %q succeeded, want it refused", path)
		}
	}
}

// TestReadableRefusesAnEmptyPath covers the call that named nothing.
func TestReadableRefusesAnEmptyPath(t *testing.T) {
	r, err := NewReadable(tree(t), nil)
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	if _, err := r.ReadablePath(""); err == nil {
		t.Error("an empty path was readable, want it refused")
	}
	if _, err := r.WritablePath(""); err == nil {
		t.Error("an empty path was writable, want it refused")
	}
}

// TestReadableWithoutAWorkingDirectoryProducesAResult covers the edge.
func TestReadableWithoutAWorkingDirectoryProducesAResult(t *testing.T) {
	var r *Readable

	if _, err := r.ReadablePath("x"); err == nil {
		t.Error("a reach with no working directory read a file, want a refusal")
	}
	if _, err := r.WritablePath("x"); err == nil {
		t.Error("a reach with no working directory wrote a file, want a refusal")
	}
	if got := r.Roots(); got != nil {
		t.Errorf("Roots on nothing is %v, want nil", got)
	}
}

// TestARootThatCannotBeResolvedIsReported covers the grant that was not opened.
//
// A reader who named a directory and got a session that quietly could not read it
// would have been told something untrue by the silence.
func TestARootThatCannotBeResolvedIsReported(t *testing.T) {
	work := tree(t)
	missing := filepath.Join(t.TempDir(), "no-such-root")

	if _, err := NewReadable(work, []string{missing}); err == nil {
		t.Error("a root that does not exist was accepted, want it reported")
	}
	if _, err := NewReadable(missing, nil); err == nil {
		t.Error("a working directory that does not exist was accepted, want it reported")
	}
}

// TestRootsReportsWhatWasGranted covers the accessor a message uses.
func TestRootsReportsWhatWasGranted(t *testing.T) {
	work := tree(t)
	granted := tree(t)

	r, err := NewReadable(work, []string{granted})
	if err != nil {
		t.Fatalf("NewReadable: %v", err)
	}

	roots := r.Roots()
	if len(roots) != 1 {
		t.Fatalf("Roots reports %v, want one entry", roots)
	}
	// The root is reported resolved, since that is the form a reader would put in
	// the file.
	resolved, err := filepath.EvalSymlinks(granted)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if roots[0] != resolved {
		t.Errorf("Roots reports %q, want the resolved form %q", roots[0], resolved)
	}
}
