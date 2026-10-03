package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree builds a directory with a few files and returns its path.
func tree(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("in "+name), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "three.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

// TestContainmentRefusesParent is the rule the package exists for.
//
// A path that walks upward is refused, and it is refused before anything is
// opened or run.
func TestContainmentRefusesParent(t *testing.T) {
	dir := tree(t)

	for _, path := range []string{
		"..",
		"../",
		"../one.txt",
		"../outside.txt",
		"../../etc/passwd",
		"a/../../outside.txt",
		"sub/../../outside.txt",
		"sub/../../../outside.txt",
		"./../one.txt",
	} {
		t.Run(path, func(t *testing.T) {
			if _, err := containment(dir, path); err == nil {
				t.Errorf("containment(%q) allowed a path out of the tree", path)
			}
		})
	}
}

// TestContainmentRefusesAnAbsolutePath covers the other way out.
//
// Stripping the leading separator would turn /etc/passwd into etc/passwd under
// the root, which is a different file from the one asked for.
func TestContainmentRefusesAnAbsolutePath(t *testing.T) {
	dir := tree(t)

	for _, path := range []string{"/etc/passwd", "/", "/tmp"} {
		if _, err := containment(dir, path); err == nil {
			t.Errorf("containment(%q) allowed an absolute path", path)
		}
	}
}

// TestContainmentAllowsInsideTheTree is the other half, so that the refusals
// cannot pass by refusing everything.
func TestContainmentAllowsInsideTheTree(t *testing.T) {
	dir := tree(t)

	for _, path := range []string{
		"one.txt",
		"./one.txt",
		"sub/three.txt",
		"sub",
		".",
		"sub/../one.txt",
		"a/../one.txt",
	} {
		t.Run(path, func(t *testing.T) {
			if _, err := containment(dir, path); err != nil {
				t.Errorf("containment(%q) refused a path inside the tree: %v", path, err)
			}
		})
	}
}

// TestContainmentAllowsAPathThatDoesNotExistYet covers creating a file.
//
// A tool that refused to write a file because the directory holding it has not
// been made would refuse most of what it is for.
func TestContainmentAllowsAPathThatDoesNotExistYet(t *testing.T) {
	dir := tree(t)

	if _, err := containment(dir, "new.txt"); err != nil {
		t.Errorf("containment refused a file that does not exist yet: %v", err)
	}
	if _, err := containment(dir, "a/b/c.txt"); err != nil {
		t.Errorf("containment refused a new nested path: %v", err)
	}
}

// TestContainmentRefusesAPathLeavingByAPrefix covers the string comparison trap.
//
// "/work/a..b" must not be judged to be inside "/work/a" by a prefix test, which
// is why the test includes the separator.
func TestContainmentRefusesAPrefixMatch(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "a")
	sibling := filepath.Join(parent, "ab")

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if within(root, sibling) {
		t.Error("a sibling sharing a name prefix was judged to be inside the root")
	}
	if !within(root, root) {
		t.Error("the root itself was not judged to be inside the root")
	}
}

// TestContainmentRefusesASymlinkLeavingTheTree is the reason the filesystem tools
// use a descriptor instead, exercised here on the comparison path.
//
// A cleaned path cannot see through a symlink, so a string check alone would
// permit a link pointing out of the tree.
func TestContainmentRefusesASymlinkLeavingTheTree(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("not yours"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dir := tree(t)
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	if _, err := containment(dir, "escape/secret.txt"); err == nil {
		t.Error("containment followed a symlink out of the tree")
	}
}

// TestContainmentRefusesAnEmptyPath covers the call with no path at all.
func TestContainmentRefusesAnEmptyPath(t *testing.T) {
	dir := tree(t)

	for _, path := range []string{"", "   "} {
		if _, err := containment(dir, path); err == nil {
			t.Errorf("containment(%q) allowed an empty path", path)
		}
	}
}

// TestEnvironmentCarriesNoCredential covers the rule about the environment.
//
// PATH is read so a program can be found, and HOME is passed so a program that
// writes a temporary file has somewhere to put it. Nothing else is given, so a
// subprocess cannot inherit a secret the reader did not intend to hand it.
func TestEnvironmentCarriesNoCredential(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/reader")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-must-not-be-passed")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "must-not-be-passed")

	env := environment()
	joined := strings.Join(env, "\n")

	if strings.Contains(joined, "sk-or-v1") {
		t.Errorf("the environment carries the credential: %q", joined)
	}
	if strings.Contains(joined, "AWS_SECRET") {
		t.Errorf("the environment carries an unrelated secret: %q", joined)
	}
	if !strings.Contains(joined, "PATH=") {
		t.Errorf("the environment has no PATH: %q", joined)
	}
	if !strings.Contains(joined, "HOME=") {
		t.Errorf("the environment has no HOME: %q", joined)
	}
	if len(env) > 2 {
		t.Errorf("the environment carries %d variables, want at most PATH and HOME: %q",
			len(env), joined)
	}
}
