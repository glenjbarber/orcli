package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellRefusesAProgramOutsideTheList covers the invariant.
//
// A program outside the list is refused by name, and the refusal names the list
// so a model that asked for something outside it learns what it may ask for.
func TestShellRefusesAProgramOutsideTheList(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	for _, command := range []string{
		"sh", "bash", "zsh", "curl", "ssh", "python", "python3", "perl", "ruby",
		"env", "xargs", "nohup", "eval", "chmod", "kill", "killall", "nc", "telnet",
	} {
		t.Run(command, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{"command": command}))
			if result.Err == nil {
				t.Errorf("%s ran, want it refused: it is not on the list", command)
			}
			if !strings.Contains(result.Err.Error(), "not permitted") {
				t.Errorf("the failure is %q, want it to say the program is not permitted",
					result.Err)
			}
			// The refusal names what may be run, so a model learns the list.
			for _, permitted := range shellPermitted {
				if !strings.Contains(result.Err.Error(), permitted) {
					t.Errorf("the refusal does not name %s: %q", permitted, result.Err)
					break
				}
			}
		})
	}
}

// TestShellPermittedListIsExactlyThirtyTwo pins the count the design names.
//
// A list that grows without a decision is a list nobody has agreed to, and the
// count is the cheapest way to notice that happening.
func TestShellPermittedListIsExactlyThirtyTwo(t *testing.T) {
	if got := len(shellPermitted); got != 32 {
		t.Errorf("the list holds %d programs, want 32", got)
	}

	seen := map[string]bool{}
	for _, name := range shellPermitted {
		if seen[name] {
			t.Errorf("%s is on the list twice", name)
		}
		seen[name] = true
	}
}

// TestShellEveryPermittedProgramIsNamedToTheModel is the model-facing check.
//
// The list is spelled out a second time in the schema description, and the two
// copies cannot be made identical by construction without producing a
// comma-separated identifier where a sentence belongs. This test is what holds
// them together.
func TestShellEveryPermittedProgramIsNamedToTheModel(t *testing.T) {
	description := NewShell(t.TempDir()).Describe().Function.Description

	for _, name := range shellPermitted {
		if !strings.Contains(description, name) {
			t.Errorf("%s is permitted but is not named in the description", name)
		}
	}

	// And nothing is named that is not permitted.
	for _, name := range []string{"sh", "bash", "curl", "ssh", "python", "rm -rf"} {
		if strings.Contains(description, name+",") && !shellPermittedMap[name] {
			t.Errorf("the description names %s, which is not permitted", name)
		}
	}
}

// TestShellRefusesFindExec is the decision you made expressly.
//
// -exec hands the program a command to run, which is the one thing an argument
// array does not protect against.
func TestShellRefusesFindExec(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	for _, flag := range []string{"-exec", "-execdir", "-fls", "-fprint"} {
		t.Run(flag, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{
				"command": "find",
				"args":    []string{".", flag, "sh", "-c", "id"},
			}))
			if result.Err == nil {
				t.Errorf("find %s ran, want it refused", flag)
			}
			if !strings.Contains(result.Err.Error(), flag) {
				t.Errorf("the failure is %q, want it to name %s", result.Err, flag)
			}
		})
	}
}

// TestShellAllowsFindDelete records the decision not to refuse -delete.
//
// It removes files and runs nothing, so it is bounded by the check on the paths
// it is given. This test exists so that the omission is a decision on the record
// rather than an oversight.
func TestShellAllowsFindDelete(t *testing.T) {
	if isRefusedOption("find", "-delete") {
		t.Error("-delete is refused, want it allowed: it runs nothing and is bounded by its paths")
	}

	// It is not refused merely because it starts with a dash either.
	if isRefusedOption("find", "-deleted") {
		t.Error("-deleted is refused, want the match to be exact")
	}
}

// TestShellRefusesFindExecWhereverItAppears checks the flag is caught at any
// position, since a reader is not going to find it first.
func TestShellRefusesFindExecWhereverItAppears(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	result := invoke(shell, body(t, map[string]any{
		"command": "find",
		"args":    []string{"-name", "*.go", "-exec", "sh", "-c", "id", ";"},
	}))
	if result.Err == nil {
		t.Error("find with -exec in a later position ran, want it refused")
	}
}

// TestShellRefusesAPathLeavingTheTree is the rule you named.
//
// Every form of walking out is refused, and refused before the process starts.
func TestShellRefusesAPathLeavingTheTree(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	for _, path := range []string{
		"..", "../", "../one.txt", "../../etc/passwd", "sub/../../outside.txt", "./../one.txt",
	} {
		t.Run(path, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{
				"command": "cat",
				"args":    []string{path},
			}))
			if result.Err == nil {
				t.Errorf("cat %q ran, want it refused", path)
			}
		})
	}
}

// TestShellRefusesAnAbsolutePath covers the decision you made.
//
// An absolute path is refused rather than held to the same bound, so a model
// that sends one is told no rather than having it made relative.
func TestShellRefusesAnAbsolutePath(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	// Even a path inside the tree is refused, since the spelling is what is
	// being refused rather than the destination.
	inside := filepath.Join(dir, "one.txt")
	result := invoke(shell, body(t, map[string]any{
		"command": "cat",
		"args":    []string{inside},
	}))
	if result.Err == nil {
		t.Errorf("cat %q ran, want it refused: an absolute path is refused outright", inside)
	}
	if !strings.Contains(result.Err.Error(), "absolute") {
		t.Errorf("the failure is %q, want it to say the path is absolute", result.Err)
	}
}

// TestShellRunsAPathInsideTheTree is the other half, so the refusals cannot pass
// by refusing everything.
func TestShellRunsAPathInsideTheTree(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	result := invoke(shell, body(t, map[string]any{"command": "cat", "args": []string{"one.txt"}}))
	if result.Err != nil {
		t.Fatalf("cat one.txt: %v", result.Err)
	}
	if got, want := result.Content, "in one.txt"; got != want {
		t.Errorf("content is %q, want %q", got, want)
	}
}

// TestShellPassesAnOptionThrough covers the rule that a dash is what separates
// an option from a path.
func TestShellPassesAnOptionThrough(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	// -I to grep is a flag, and treating it as a file name would refuse a
	// perfectly ordinary call.
	result := invoke(shell, body(t, map[string]any{
		"command": "grep",
		"args":    []string{"-I", "in", "one.txt"},
	}))
	if result.Err != nil {
		t.Errorf("grep -I: %v", result.Err)
	}
}

// TestShellNeverReachesAShell covers the argument array.
//
// An argument carrying shell syntax is a string. Nothing in it is interpreted,
// because nothing here is run by a shell.
func TestShellNeverReachesAShell(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)

	result := invoke(shell, body(t, map[string]any{
		"command": "echo",
		"args":    []string{"a > pwned.txt", "&& rm -rf /", "$(whoami)", "`id`"},
	}))
	if result.Err != nil {
		t.Fatalf("echo: %v", result.Err)
	}

	for _, want := range []string{"> pwned.txt", "&& rm -rf /", "$(whoami)", "`id`"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("the output is %q, want the argument %q unchanged", result.Content, want)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "pwned.txt")); !os.IsNotExist(err) {
		t.Error("a redirect was interpreted, so an argument reached a shell")
	}
}

// TestShellRunsInTheWorkingDirectory checks where the process is put.
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
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", got, err)
	}
	if gotResolved != want {
		t.Errorf("pwd reported %q, want %q", got, dir)
	}
}

// TestShellReportsAProgramThatIsNotFound covers an allowlisted name that is not
// installed.
//
// The list says what may be proposed; PATH says what exists. A name on the list
// that is not on the disk is a fault to report, not a silent success.
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

	for _, fields := range []map[string]any{{}, {"command": ""}, {"command": "   "}} {
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
	if result := invoke(NewShell(""), body(t, map[string]any{"command": "ls"})); result.Err == nil {
		t.Error("a shell with an empty working directory succeeded, want a refusal")
	}
}

// TestShellRefusesArgumentsThatAreNotAnObject covers the shared decode.
func TestShellRefusesArgumentsThatAreNotAnObject(t *testing.T) {
	shell := NewShell(tree(t))

	if result := invoke(shell, json.RawMessage(`"ls"`)); result.Err == nil {
		t.Error("a string of arguments was accepted, want a refusal")
	}
}

// TestShellSaysItIsNotAShell checks the schema tells a model the truth.
func TestShellSaysItIsNotAShell(t *testing.T) {
	description := NewShell(t.TempDir()).Describe().Function.Description

	for _, want := range []string{
		"not a shell", "pipes", "relative to the working directory",
		"gh and rm write", "may not be given -exec",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("the description does not say %q: %s", want, description)
		}
	}
}

// TestShellChecksTheListBeforeAskingAboutTheDirectory covers the order.
//
// A program that is not permitted is refused before the filesystem is asked about
// it, since a call that was never going to run is not something to look up.
func TestShellChecksTheListBeforeTheFilesystem(t *testing.T) {
	// A directory that does not exist, and a program that is not permitted.
	shell := NewShell(filepath.Join(t.TempDir(), "no-such-dir"))

	result := invoke(shell, body(t, map[string]any{"command": "definitely-not-permitted"}))
	if result.Err == nil {
		t.Fatal("the call succeeded, want a refusal")
	}
	if !strings.Contains(result.Err.Error(), "not permitted") {
		t.Errorf("the failure is %q, want the refusal to be about the program rather than the path",
			result.Err)
	}
}
