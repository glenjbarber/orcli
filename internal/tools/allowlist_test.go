package tools

import (
	"strings"
	"testing"
)

// TestEveryPermittedProgramHasAShape is what makes the list expandable.
//
// A person adding a program touches two places: the list and the shape table. A
// program on the list with no shape would have its arguments checked against nothing,
// so this test refuses that rather than trusting it.
func TestEveryPermittedProgramHasAShape(t *testing.T) {
	for _, name := range shellPermitted {
		if _, ok := pathShapes[name]; !ok {
			t.Errorf("%s is permitted with no declared path shape", name)
		}
	}
}

// TestNoShapeWithoutAPermittedProgram checks the other direction.
//
// A shape for a program that is not permitted is either a typo or a program someone
// meant to add and did not, and both are worth finding.
func TestNoShapeWithoutAPermittedProgram(t *testing.T) {
	for name := range pathShapes {
		if !shellPermittedMap[name] {
			t.Errorf("%s has a path shape but is not permitted", name)
		}
	}
}

// TestTheShellPermittedMapMatchesItsList holds the derived form to the declaration.
func TestTheShellPermittedMapMatchesItsList(t *testing.T) {
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

// TestTheDescriptionNamesEveryPermittedProgram is what keeps the two copies
// together.
//
// The description a model reads is rendered from the list rather than written out
// beside it, so a program added to the list cannot be missing from it. This test is
// the belt to that braces: it checks the rendering actually names every program.
func TestTheDescriptionNamesEveryPermittedProgram(t *testing.T) {
	description := NewShell(t.TempDir()).Describe().Function.Description

	for _, name := range shellPermitted {
		if !strings.Contains(description, name) {
			t.Errorf("%s is permitted but is not named in the description", name)
		}
	}
	if !strings.Contains(description, "not a shell") {
		t.Error("the description does not say the tool is not a shell")
	}
}

// TestTheDescriptionNamesNoProgramThatIsNotPermitted checks the other direction.
//
// The check is on the listed names rather than on a substring anywhere in the text,
// since sh appears inside shell and rm inside rm: a substring test would report both
// as named when they are named as parts of other words.
func TestTheDescriptionNamesNoProgramThatIsNotPermitted(t *testing.T) {
	description := NewShell(t.TempDir()).Describe().Function.Description

	// The named programs are the ones between the first colon and the period, so
	// the check is on that run rather than on the whole description.
	start := strings.Index(description, ": ")
	if start < 0 {
		t.Fatalf("the description names no list: %s", description)
	}
	rest := description[start+2:]
	end := strings.Index(rest, ".")
	if end < 0 {
		t.Fatalf("the description names no closing period: %s", description)
	}
	listed := strings.Split(rest[:end], ",")

	for _, name := range listed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !shellPermittedMap[name] {
			t.Errorf("the description names %q, which is not permitted", name)
		}
	}

	if len(listed) != len(shellPermitted) {
		t.Errorf("the description names %d programs, the list holds %d",
			len(listed), len(shellPermitted))
	}
}

// TestEchoTakesNoPath covers the shape that caught a real defect.
//
// echo prints its arguments, so resolving one as a path replaces the text a model wrote
// with a directory it did not name. That is a change to the call rather than a check on
// it.
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

	out, err := shell.checkArgs("echo", arguments)
	if err != nil {
		t.Fatalf("echo with shell syntax: %v", err)
	}

	for i, want := range arguments {
		if out[i] != want {
			t.Errorf("argument %d became %q, want it unchanged as %q", i, out[i], want)
		}
	}
}

// TestAPatternIsNotARewrittenPath covers grep, whose file is not its first argument.
//
// A pattern is not a path, and resolving one would replace it with a directory.
func TestAPatternIsNotARewrittenPath(t *testing.T) {
	shell := NewShell(tree(t))

	out, err := shell.checkArgs("grep", []string{"-I", "in one.txt", "one.txt"})
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

// TestAPatternWithNoDirectoryIsJudgedWhereItExpands covers cutWildcard.
//
// *.go is judged against the directory it will be expanded in and is allowed, since that
// is the ordinary form a shell would expand. A pattern with no directory in front of it
// yields an empty head, which resolves to the working directory, so it is not refused.
func TestAPatternWithNoDirectoryIsJudgedWhereItExpands(t *testing.T) {
	shell := NewShell(tree(t))

	out, err := shell.checkArgs("find", []string{".", "*.go"})
	if err != nil {
		t.Errorf("find . *.go was refused: %v", err)
	}
	if len(out) == 2 && out[1] != "*.go" {
		t.Errorf("the pattern became %q, want it unchanged", out[1])
	}
}

// TestAPatternLeavingTheTreeIsRefused covers the same rule from the other side.
//
// ../*.go names a directory that is not in the tree, so it is refused. Cleaning it
// would produce ../*.go as a path, which is a different thing from the files a shell
// would have matched.
func TestAPatternLeavingTheTreeIsRefused(t *testing.T) {
	shell := NewShell(tree(t))

	for _, pattern := range []string{"../*.go", "../../etc/*", "sub/../../*"} {
		t.Run(pattern, func(t *testing.T) {
			if _, err := shell.checkArgs("find", []string{pattern}); err == nil {
				t.Errorf("find %q was accepted, want it refused", pattern)
			}
		})
	}
}

// TestRefusedOptionsAreCaughtAtAnyPosition covers a reader not finding it first.
//
// The refused option is placed after the directory find needs, so the position under test
// is the position the flag was written to rather than an overwrite of the directory by
// the test itself.
func TestRefusedOptionsAreCaughtAtAnyPosition(t *testing.T) {
	shell := NewShell(tree(t))

	for _, at := range []int{1, 2, 4} {
		arguments := []string{".", "-name", "*.go", "-exec", "sh", "-c", "id", ";"}
		arguments[at] = "-exec"

		if _, err := shell.checkArgs("find", arguments); err == nil {
			t.Errorf("-exec at position %d was accepted, want it refused", at)
		}
	}
}

// TestRefusedOptionIsCaughtBeforeTheFirstArgument covers a find given the flag with no
// directory at all, which is a malformed call that still must not run.
func TestRefusedOptionIsCaughtBeforeTheFirstArgument(t *testing.T) {
	shell := NewShell(tree(t))

	if _, err := shell.checkArgs("find", []string{"-exec", "sh", "-c", "id", ";"}); err == nil {
		t.Error("find -exec with no directory was accepted, want it refused")
	}
}

// TestAPermittedProgramWithNoShapeIsRefused covers the gap.
//
// A gap in the table is not a licence to run a program whose arguments nobody has thought
// about, so the call is refused rather than run unchecked.
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

	if _, err := shell.checkArgs(name, []string{"x"}); err == nil {
		t.Errorf("%s ran with no declared shape, want it refused", name)
	}
}
