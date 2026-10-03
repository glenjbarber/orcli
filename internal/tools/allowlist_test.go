package tools

import (
	"strings"
	"testing"
)

// TestEveryPermittedProgramHasAShape is what makes the list expandable.
//
// A person adding a program touches three places: the list, the shape table, and
// the description. A program on the list with no shape would have its arguments
// checked against nothing, so this test refuses that rather than trusting it.
func TestEveryPermittedProgramHasAShape(t *testing.T) {
	for _, name := range shellPermitted {
		if _, ok := pathShapes[name]; !ok {
			t.Errorf("%s is permitted with no declared path shape", name)
		}
	}
}

// TestNoShapeWithoutAPermittedProgram checks the other direction.
//
// A shape for a program that is not permitted is either a typo or a program
// someone meant to add and did not, and both are worth finding.
func TestNoShapeWithoutAPermittedProgram(t *testing.T) {
	for name := range pathShapes {
		if !shellPermittedMap[name] {
			t.Errorf("%s has a path shape but is not permitted", name)
		}
	}
}

// TestThePermittedMapMatchesTheList holds the derived form to the declaration.
func TestThePermittedMapMatchesTheList(t *testing.T) {
	if len(shellPermittedMap) != len(shellPermitted) {
		t.Errorf("the map holds %d names, the list holds %d",
			len(shellPermittedMap), len(shellPermitted))
	}
	for _, name := range shellPermitted {
		if !shellPermittedMap[name] {
			t.Errorf("%s is on the list and not in the map", name)
		}
	}
}

// TestEchoTakesNoPath covers the shape that caught a real defect.
//
// echo prints its arguments, so resolving one as a path replaces the text a model
// wrote with a directory it did not name. That is a change to the call rather than
// a check on it.
func TestEchoTakesNoPath(t *testing.T) {
	shell := NewShell(tree(t))

	arguments := []string{
		"a > pwned.txt",
		"&& rm -rf /",
		"$(whoami)",
		"`id`",
		"../outside",
		"/etc/passwd",
		"hello",
	}

	_, out, err := shell.check("echo", arguments)
	if err != nil {
		t.Fatalf("echo with shell syntax: %v", err)
	}

	for i, want := range arguments {
		if out[i] != want {
			t.Errorf("argument %d became %q, want it unchanged as %q", i, out[i], want)
		}
	}
}

// TestAPatternIsNotARewrittenPath covers grep, whose file is not its first
// argument.
//
// A pattern is not a path, and resolving one would replace it with a directory.
func TestAPatternIsNotARewrittenPath(t *testing.T) {
	shell := NewShell(tree(t))

	arguments := []string{"-I", "in one.txt", "one.txt"}
	_, out, err := shell.check("grep", arguments)
	if err != nil {
		t.Fatalf("grep: %v", err)
	}

	if out[0] != "-I" {
		t.Errorf("the flag became %q, want it unchanged", out[0])
	}
	if out[1] != "in one.txt" {
		t.Errorf("the pattern became %q, want it unchanged", out[1])
	}
	if !strings.HasSuffix(out[2], "one.txt") {
		t.Errorf("the file became %q, want it resolved under the tree", out[2])
	}
}

// TestFindChecksOnlyItsFirstArgument covers the shape that keeps a pattern from
// being rewritten.
//
// find takes a directory and then patterns and flags, and only the directory is a
// path. Resolving the rest would refuse ordinary calls, since a pattern like *.go
// names something that has not been resolved yet.
func TestFindChecksOnlyItsFirstArgument(t *testing.T) {
	shell := NewShell(tree(t))

	arguments := []string{".", "-name", "*.go", "-delete"}
	_, out, err := shell.check("find", arguments)
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	if out[1] != "-name" || out[2] != "*.go" || out[3] != "-delete" {
		t.Errorf("the arguments after the first became %v, want them unchanged", out[1:])
	}
}

// TestRefusedOptionsAreCaughtAtAnyPosition covers a reader not finding it first.
//
// The refused option is placed after the directory find needs, so the position
// under test is the position the flag was written to rather than an overwrite of
// the directory by the test itself.
func TestRefusedOptionsAreCaughtAtAnyPosition(t *testing.T) {
	shell := NewShell(tree(t))

	for _, at := range []int{1, 2, 4} {
		arguments := []string{".", "-name", "*.go", "-exec", "sh", "-c", "id", ";"}
		arguments[at] = "-exec"

		if _, _, err := shell.check("find", arguments); err == nil {
			t.Errorf("-exec at position %d was accepted, want it refused", at)
		}
	}
}

// TestRefusedOptionIsCaughtBeforeTheFirstArgument covers a find given the flag
// with no directory at all, which is a malformed call that still must not run.
func TestRefusedOptionIsCaughtBeforeTheFirstArgument(t *testing.T) {
	shell := NewShell(tree(t))

	if _, _, err := shell.check("find", []string{"-exec", "sh", "-c", "id", ";"}); err == nil {
		t.Error("find -exec with no directory was accepted, want it refused")
	}
}

// TestAPermittedProgramWithNoShapeIsRefused covers the gap.
//
// A gap in the table is not a licence to run a program whose arguments nobody has
// thought about, so the call is refused rather than run unchecked.
func TestAPermittedProgramWithNoShapeIsRefused(t *testing.T) {
	shell := NewShell(t.TempDir())

	// The shape is removed rather than a name invented, so the test exercises the
	// gap rather than the list check.
	name := shellPermitted[0]
	shape, ok := pathShapes[name]
	if !ok {
		t.Fatalf("%s has no shape to remove", name)
	}
	delete(pathShapes, name)
	t.Cleanup(func() { pathShapes[name] = shape })

	if _, _, err := shell.check(name, []string{"x"}); err == nil {
		t.Errorf("%s ran with no declared shape, want it refused", name)
	}
}
