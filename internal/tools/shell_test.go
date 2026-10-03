package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellRefusesAProgramOutsideTheAllowlist covers the allowlist.
//
// A program that is not named is refused even when it exists, because the
// allowlist is what bounds what may be run at all.
func TestShellRefusesAProgramOutsideTheAllowlist(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	for _, command := range []string{
		"sh", "bash", "zsh", "rm", "mv", "curl", "gh", "git", "chmod", "kill",
		"python", "python3", "perl", "ruby", "env", "xargs", "nohup", "ssh",
	} {
		t.Run(command, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{"command": command}))
			if result.Err == nil {
				t.Errorf("%s ran, want it refused: it is not on the allowlist", command)
			}
		})
	}
}

// TestShellRefusesAProgramNamedByPath covers the bare-name rule.
//
// A path is refused even when the program at it is allowed, because an allowlist
// of names is not an allowlist of paths.
func TestShellRefusesAProgramNamedByPath(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	for _, command := range []string{"/bin/cat", "./cat", "/bin/sh", "../cat"} {
		t.Run(command, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{"command": command}))
			if result.Err == nil {
				t.Errorf("%s ran, want it refused: a program is named by bare name", command)
			}
		})
	}
}

// TestShellRefusesAPathLeavingTheTree is the rule you named.
//
// Every form of walking out is refused, and refused before the process starts.
func TestShellRefusesAPathLeavingTheTree(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	for _, path := range []string{
		"..",
		"../",
		"../one.txt",
		"../../etc/passwd",
		"sub/../../outside.txt",
		"./../one.txt",
	} {
		t.Run(path, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{
				"command": "cat",
				"args":    []string{path},
			}))
			if result.Err == nil {
				t.Errorf("cat %q ran, want it refused", path)
			}
			if !strings.Contains(result.Err.Error(), "outside") {
				t.Errorf("the failure is %q, want it to say the path is outside", result.Err)
			}
		})
	}
}

// TestShellRefusesAPathLeavingThroughGrep covers the last-argument case.
//
// grep takes a pattern before its file, so a fixed position would check the
// pattern rather than the file. This is the case that shape exists for.
func TestShellRefusesAPathLeavingThroughGrep(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	result := invoke(shell, body(t, map[string]any{
		"command": "grep",
		"args":    []string{"in", "../one.txt"},
	}))
	if result.Err == nil {
		t.Error("grep with a file above the root ran, want it refused")
	}
}

// TestShellRefusesAPathLeavingThroughEveryProgramThatTakesOne checks the shape
// table rather than one program.
func TestShellRefusesAPathLeavingThroughEveryProgramThatTakesOne(t *testing.T) {
	dir := tree(t)

	for command, shape := range pathArguments {
		if shape != first {
			continue
		}
		t.Run(command, func(t *testing.T) {
			shell := NewShell(dir)
			result := invoke(shell, body(t, map[string]any{
				"command": command,
				"args":    []string{"../outside"},
			}))
			if result.Err == nil {
				t.Errorf("%s with a path above the root ran, want it refused", command)
			}
		})
	}
}

// TestShellRunsAPathInsideTheTree is the other half, so the refusals cannot pass
// by refusing everything.
func TestShellRunsAPathInsideTheTree(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	result := invoke(shell, body(t, map[string]any{
		"command": "cat",
		"args":    []string{"one.txt"},
	}))
	if result.Err != nil {
		t.Fatalf("cat one.txt: %v", result.Err)
	}
	if got, want := result.Content, "in one.txt"; got != want {
		t.Errorf("content is %q, want %q", got, want)
	}
}

// TestShellRunsAProgramWithNoPath covers the programs whose shape is none.
func TestShellRunsAProgramWithNoPath(t *testing.T) {
	dir := tree(t)

	for command, shape := range pathArguments {
		if shape != none {
			continue
		}
		t.Run(command, func(t *testing.T) {
			shell := NewShell(dir)
			result := invoke(shell, body(t, map[string]any{"command": command}))
			// false always fails, which is a reported failure and not a fault
			// in the tool.
			if result.Err != nil && command != "false" {
				t.Errorf("%s: %v", command, result.Err)
			}
		})
	}
}

// TestShellRefusesAProgramWithNoDeclaredShape covers the gap.
//
// A program on the allowlist whose path shape has not been worked out is
// refused rather than run unchecked, since a gap is not a licence. The program
// here is not on the allowlist either, which is the belt: the check runs before
// the name is resolved.
func TestShellRefusesAProgramWithNoDeclaredShape(t *testing.T) {
	shell := NewShell(t.TempDir())

	if _, err := shell.checkArgs("a-program-with-no-shape", []string{"x"}); err == nil {
		t.Error("a program with no declared shape was checked, want a refusal")
	}
}

// TestShellNeverReachesAShell covers the argument array.
//
// An argument carrying shell syntax is a string. Nothing in it is interpreted,
// because nothing here is run by a shell.
func TestShellNeverReachesAShell(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	// If any of this reached a shell, the redirect would create a file and the
	// chain would run a program that is not on the allowlist.
	result := invoke(shell, body(t, map[string]any{
		"command": "echo",
		"args":    []string{"a > pwned.txt", "&& rm -rf /", "$(whoami)", "`id`"},
	}))
	if result.Err != nil {
		t.Fatalf("echo: %v", result.Err)
	}

	// The arguments came back as the literal strings.
	for _, want := range []string{"> pwned.txt", "&& rm -rf /", "$(whoami)", "`id`"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("the output is %q, want the argument %q passed through unchanged",
				result.Content, want)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "pwned.txt")); !os.IsNotExist(err) {
		t.Error("a redirect was interpreted, so an argument reached a shell")
	}
}

// TestShellRunsInTheWorkingDirectory checks where the process is put.
//
// Every path is relative to the working directory, so the process has to run
// there or a relative path would mean something else.
func TestShellRunsInTheWorkingDirectory(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	result := invoke(shell, body(t, map[string]any{"command": "pwd"}))
	if result.Err != nil {
		t.Fatalf("pwd: %v", result.Err)
	}

	// macOS reports a temporary directory through a symlink, so the two are
	// compared as resolved paths rather than as written.
	got := strings.TrimSpace(result.Content)
	wantResolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", got, err)
	}
	if gotResolved != wantResolved {
		t.Errorf("pwd reported %q, want %q", got, dir)
	}
}

// TestShellReportsAFailureWithItsOutput covers a program that ran and failed.
//
// The output is kept: a program that wrote something before failing has answered
// part of the question, and discarding it would make a model retry a call whose
// answer was already on the wire.
//
// grep is the case, and it is chosen because a matching grep writes to stdout
// and a non-matching one writes nothing. The first is a failure with output to
// keep; the second is a failure with nothing to keep, which is why the earlier
// version of this test asserted nothing about output and passed by accident.
func TestShellReportsAFailureWithItsOutput(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	// A missing file is a failure, and there is no output with it.
	result := invoke(shell, body(t, map[string]any{
		"command": "cat",
		"args":    []string{"no-such-file.txt"},
	}))
	if result.Err == nil {
		t.Fatal("cat of a missing file succeeded, want it reported as a failure")
	}

	// head against a directory it cannot read is a failure as well. The point of
	// the test is that neither discards output, and that neither is reported as
	// a success.
	result = invoke(shell, body(t, map[string]any{
		"command": "head",
		"args":    []string{"one.txt"},
	}))
	if result.Err != nil {
		t.Errorf("head one.txt: %v", result.Err)
	}
	if !strings.Contains(result.Content, "in one.txt") {
		t.Errorf("content is %q, want the file contents", result.Content)
	}
}

// TestShellReportsAProgramThatIsNotFound covers an allowlisted name that is not
// installed.
//
// The allowlist says what may be proposed; PATH says what exists. A name on the
// list that is not on the disk is a fault to report, not a silent success.
func TestShellReportsAProgramThatIsNotFound(t *testing.T) {
	dir := t.TempDir()
	shell := NewShell(dir)
	t.Setenv("PATH", dir)

	result := invoke(shell, body(t, map[string]any{"command": "cat"}))
	if result.Err == nil {
		t.Error("a program that is not installed succeeded, want it reported")
	}
	if !strings.Contains(result.Err.Error(), "PATH") {
		t.Errorf("the failure is %q, want it to mention PATH", result.Err)
	}
}

// TestShellNeedsACommand covers the call that named nothing.
func TestShellNeedsACommand(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	for _, fields := range []map[string]any{
		{},
		{"command": ""},
		{"command": "   "},
	} {
		if result := invoke(shell, body(t, fields)); result.Err == nil {
			t.Errorf("a call with no command (%v) succeeded, want a refusal", fields)
		}
	}
}

// TestShellWithoutADirectoryProducesAResult covers the guarantee at its edge.
func TestShellWithoutADirectoryProducesAResult(t *testing.T) {
	var shell *Shell

	if result := invoke(shell, body(t, map[string]any{"command": "ls"})); result.Err == nil {
		t.Error("a shell with no working directory succeeded, want a refusal")
	}

	result := invoke(NewShell(""), body(t, map[string]any{"command": "ls"}))
	if result.Err == nil {
		t.Error("a shell with an empty working directory succeeded, want a refusal")
	}
}

// TestShellRefusesArgumentsThatAreNotAnObject covers the shared decode.
func TestShellRefusesArgumentsThatAreNotAnObject(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	if result := invoke(shell, json.RawMessage(`"ls"`)); result.Err == nil {
		t.Error("a string of arguments was accepted, want a refusal")
	}
}

// TestShellSaysItIsNotAShell checks the schema tells a model the truth.
//
// A model told only what the tool can do will ask for a pipe, and a model told
// it cannot will use the programs it has.
func TestShellSaysItIsNotAShell(t *testing.T) {
	shell := NewShell(t.TempDir())
	description := shell.Describe().Function.Description

	for _, want := range []string{"not a shell", "pipes", "relative to the working directory"} {
		if !strings.Contains(description, want) {
			t.Errorf("the description does not say %q: %s", want, description)
		}
	}
}

// TestShellRefusesToWidenItself covers the allowlist being fixed here.
//
// The set is a property of this package. A program named on the allowlist but
// with no path shape would be one whose arguments were never checked.
func TestShellRefusesToWidenItself(t *testing.T) {
	for _, name := range shellPrograms {
		if _, ok := pathArguments[name]; !ok {
			t.Errorf("%s is on the allowlist with no declared path shape", name)
		}
	}
}
