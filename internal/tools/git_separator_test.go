package tools

import (
	"strings"
	"testing"
)

// TestGitPlacesASeparatorBeforeAPath covers the requirement the design names.
//
// A -- precedes any pathspec, so an argument that looks like a flag cannot become one
// by being read as a pathspec. The paths themselves are checked to be resolved, since
// a separator alone would stop git reading a flag without stopping it reading a path
// outside the tree.
func TestGitPlacesASeparatorBeforeAPath(t *testing.T) {
	dir := repo(t)
	g := NewGit(dir)

	_, args, err := g.check("add", []string{"one.txt", "two.txt"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if len(args) < 2 || args[1] != "--" {
		t.Fatalf("the arguments are %v, want a -- before the first path", args)
	}
	if args[0] != "add" {
		t.Errorf("the subcommand is %q, want it first and untouched", args[0])
	}

	// Every path after the separator is absolute, so it cannot be read relative to
	// whatever directory the caller happened to be in.
	resolvedDir, err := resolveRoot(dir)
	if err != nil {
		t.Fatalf("resolveRoot: %v", err)
	}
	for _, arg := range args[2:] {
		if !strings.HasPrefix(arg, resolvedDir) {
			t.Errorf("the path %q was not resolved under the working directory", arg)
		}
	}
}
