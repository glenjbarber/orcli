package tools

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitBin is the git this suite arranges repositories with. It is resolved once and the
// suite skips rather than fails when git is not installed, since the properties under
// test are git's own: what a subcommand accepts, what a pathspec is, and how -- is
// read.
var gitBin, gitErr = exec.LookPath("git")

// gitIn runs git in a directory, for arranging a test rather than for testing one.
//
// The arguments go in as an array and the environment is set explicitly, so nothing in
// a test's own argument reaches a shell.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	if gitErr != nil {
		t.Skipf("git is not installed: %v", gitErr)
	}

	cmd := exec.Command(gitBin, args...)
	cmd.Dir = dir
	cmd.Env = environment()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repo builds a git repository with one commit and returns its path.
func repo(t *testing.T) string {
	t.Helper()

	if gitErr != nil {
		t.Skipf("git is not installed: %v", gitErr)
	}

	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "test@example.invalid")
	gitIn(t, dir, "config", "user.name", "Test")

	for _, name := range []string{"one.txt", "two.txt", "three.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content of "+name), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	gitIn(t, dir, "add", "one.txt", "two.txt", "three.go")
	gitIn(t, dir, "commit", "-q", "-m", "first")
	return dir
}

// TestGitRefusesASubcommandOutsideTheList covers the allowlist.
//
// A model asking for something not here is refused by name and learns what it may ask
// for.
func TestGitRefusesASubcommandOutsideTheList(t *testing.T) {
	g := NewGit(repo(t))

	for _, subcommand := range []string{
		"clean", "reset", "filter-branch", "filter-repo", "gc", "prune", "am",
		"submodule", "credential", "credential-cache", "daemon", "upload-pack",
	} {
		t.Run(subcommand, func(t *testing.T) {
			result := invoke(g, body(t, map[string]any{"subcommand": subcommand}))
			if result.Err == nil {
				t.Errorf("git %s ran, want it refused", subcommand)
			}
		})
	}
}

// TestForbiddenAndUnknownAreRefusedDifferently checks the two refusals differ in
// wording.
//
// A model told a subcommand is unknown will try another spelling; a model told this
// tool refuses it will move on.
func TestForbiddenAndUnknownAreRefusedDifferently(t *testing.T) {
	g := NewGit(repo(t))

	forbidden := invoke(g, body(t, map[string]any{"subcommand": "clean"}))
	unknown := invoke(g, body(t, map[string]any{"subcommand": "not-a-subcommand"}))

	if forbidden.Err == nil || unknown.Err == nil {
		t.Fatal("both calls should have been refused")
	}
	if forbidden.Err.Error() == unknown.Err.Error() {
		t.Errorf("a forbidden and an unknown subcommand are refused identically: %q",
			forbidden.Err)
	}
	if !strings.Contains(forbidden.Err.Error(), "not run by this tool") {
		t.Errorf("a forbidden subcommand is refused as %q, want it named as refused",
			forbidden.Err)
	}
	if !strings.Contains(unknown.Err.Error(), "not permitted") {
		t.Errorf("an unknown subcommand is refused as %q, want it named as not permitted",
			unknown.Err)
	}
}

// TestGitRefusalNamesWhatItRuns checks the refusal teaches the model.
func TestGitRefusalNamesWhatItRuns(t *testing.T) {
	g := NewGit(repo(t))

	result := invoke(g, body(t, map[string]any{"subcommand": "not-a-subcommand"}))
	if result.Err == nil {
		t.Fatal("the call succeeded, want a refusal")
	}
	for _, permitted := range gitPermitted {
		if !strings.Contains(result.Err.Error(), permitted) {
			t.Errorf("the refusal does not name %s: %q", permitted, result.Err)
			break
		}
	}
}

// TestGitRefusesAWildcard covers the pattern case.
//
// A pattern is not a path, and cleaning one produces a path that is not a file the
// pattern would have matched, so it is refused by name.
func TestGitRefusesAWildcard(t *testing.T) {
	g := NewGit(repo(t))

	for _, pattern := range []string{"*.go", "?.txt", "[ab].txt", "**/*.go", "sub/*"} {
		t.Run(pattern, func(t *testing.T) {
			result := invoke(g, body(t, map[string]any{
				"subcommand": "add",
				"args":       []string{pattern},
			}))
			if result.Err == nil {
				t.Errorf("git add %q ran, want it refused", pattern)
			}
			if !strings.Contains(result.Err.Error(), "wildcard") {
				t.Errorf("the failure is %q, want it to say the argument is a wildcard",
					result.Err)
			}
		})
	}
}

// TestGitRefusesAWholeRepositoryOption covers the option that widens a pathspec.
func TestGitRefusesAWholeRepositoryOption(t *testing.T) {
	g := NewGit(repo(t))

	for _, option := range []string{"--all", "--glob", "--branches", "--tags", "--remotes"} {
		t.Run(option, func(t *testing.T) {
			result := invoke(g, body(t, map[string]any{
				"subcommand": "add",
				"args":       []string{option, "one.txt"},
			}))
			if result.Err == nil {
				t.Errorf("git add %s ran, want it refused", option)
			}
		})
	}
}

// TestGitRefusesAPathLeavingTheTree is the rule that applies here too.
func TestGitRefusesAPathLeavingTheTree(t *testing.T) {
	g := NewGit(repo(t))

	for _, path := range []string{
		"..", "../outside.txt", "../../etc/passwd", "sub/../../outside.txt", "/etc/passwd",
	} {
		t.Run(path, func(t *testing.T) {
			result := invoke(g, body(t, map[string]any{
				"subcommand": "add",
				"args":       []string{path},
			}))
			if result.Err == nil {
				t.Errorf("git add %q ran, want it refused", path)
			}
		})
	}
}

// TestGitRunsACommandThatTouchesOnlyTheTree is the other half, so the refusals cannot
// pass by refusing everything.
func TestGitRunsACommandThatTouchesOnlyTheTree(t *testing.T) {
	g := NewGit(repo(t))

	result := invoke(g, body(t, map[string]any{"subcommand": "status"}))
	if result.Err != nil {
		t.Fatalf("git status: %v", result.Err)
	}

	result = invoke(g, body(t, map[string]any{"subcommand": "log", "args": []string{"--oneline"}}))
	if result.Err != nil {
		t.Fatalf("git log: %v", result.Err)
	}
	if !strings.Contains(result.Content, "first") {
		t.Errorf("the log is %q, want the commit message", result.Content)
	}
}

// TestGitReportsAFailureWithItsOutput covers a command that ran and failed.
func TestGitReportsAFailureWithItsOutput(t *testing.T) {
	g := NewGit(repo(t))

	result := invoke(g, body(t, map[string]any{
		"subcommand": "commit",
		"args":       []string{"-m", "nothing"},
	}))
	if result.Err == nil {
		t.Error("committing with nothing staged succeeded, want it reported as a failure")
	}
}

// TestGitNeedsASubcommand covers the call that named nothing.
func TestGitNeedsASubcommand(t *testing.T) {
	g := NewGit(repo(t))

	for _, fields := range []map[string]any{{}, {"subcommand": ""}, {"subcommand": "  "}} {
		if result := invoke(g, body(t, fields)); result.Err == nil {
			t.Errorf("a call with no subcommand (%v) succeeded, want a refusal", fields)
		}
	}
}

// TestGitWithoutADirectoryProducesAResult covers the guarantee at its edge.
func TestGitWithoutADirectoryProducesAResult(t *testing.T) {
	var g *Git

	if result := invoke(g, body(t, map[string]any{"subcommand": "status"})); result.Err == nil {
		t.Error("a git tool with no working directory succeeded, want a refusal")
	}
	if result := invoke(NewGit(""), body(t, map[string]any{"subcommand": "status"})); result.Err == nil {
		t.Error("a git tool with an empty working directory succeeded, want a refusal")
	}
}

// TestGitRefusesArgumentsThatAreNotAnObject covers the shared decode.
func TestGitRefusesArgumentsThatAreNotAnObject(t *testing.T) {
	g := NewGit(repo(t))

	if result := invoke(g, json.RawMessage(`"status"`)); result.Err == nil {
		t.Error("a string of arguments was accepted, want a refusal")
	}
}

// TestGitChecksTheSubcommandBeforeTheFilesystem covers the order.
func TestGitChecksTheSubcommandBeforeTheFilesystem(t *testing.T) {
	// A directory that does not exist, and a subcommand that is refused.
	g := NewGit(filepath.Join(t.TempDir(), "no-such-dir"))

	result := invoke(g, body(t, map[string]any{"subcommand": "clean"}))
	if result.Err == nil {
		t.Fatal("the call succeeded, want a refusal")
	}
	if !strings.Contains(result.Err.Error(), "not run by this tool") {
		t.Errorf("the failure is %q, want the refusal to be about the subcommand", result.Err)
	}
}

// TestGitNeverReachesAShell covers the argument array.
func TestGitNeverReachesAShell(t *testing.T) {
	g := NewGit(repo(t))

	// If anything here reached a shell, the chain would run something else.
	result := invoke(g, body(t, map[string]any{
		"subcommand": "log",
		"args":       []string{"--oneline; rm -rf /", "$(whoami)", "`id`"},
	}))
	if result.Err == nil {
		t.Error("git log with shell syntax succeeded, want it refused")
	}
}

// TestEveryPermittedSubcommandIsNamedToTheModel holds the two copies together.
func TestEveryPermittedSubcommandIsNamedToTheModel(t *testing.T) {
	description := NewGit(t.TempDir()).Describe().Function.Description

	for _, name := range gitPermitted {
		if !strings.Contains(description, name) {
			t.Errorf("%s is permitted but is not named in the description", name)
		}
	}
}

// TestThePermittedMapMatchesTheList holds the derived form to the declaration.
func TestThePermittedMapMatchesTheList(t *testing.T) {
	if len(gitPermittedMap) != len(gitPermitted) {
		t.Errorf("the map holds %d names, the list holds %d",
			len(gitPermittedMap), len(gitPermitted))
	}
	for _, name := range gitPermitted {
		if !gitPermittedMap[name] {
			t.Errorf("%s is on the list and not in the map", name)
		}
	}
}

// TestNoSubcommandIsBothPermittedAndRefused catches a contradiction in the tables.
func TestNoSubcommandIsBothPermittedAndRefused(t *testing.T) {
	for name := range gitRefusedSubcommands {
		if gitPermittedMap[name] {
			t.Errorf("%s is permitted and refused at once", name)
		}
	}
}

// TestARefusalCarriesTheSentinel checks the error is tellable apart.
func TestARefusalCarriesTheSentinel(t *testing.T) {
	g := NewGit(repo(t))

	result := invoke(g, body(t, map[string]any{"subcommand": "clean"}))
	if !errors.Is(result.Err, ErrRefusedGit) {
		t.Errorf("the failure is %v, want it to carry ErrRefusedGit", result.Err)
	}
}
