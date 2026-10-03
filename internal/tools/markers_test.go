package tools

import (
	"errors"
	"strings"
	"testing"
)

// TestGitThroughTheShellIsRefusedForARefusedSubcommand is the hole the tools design
// names.
//
// git is on the shell allowlist, so without this every refusal the git tool makes is
// reachable by asking the shell instead. A model that read one refusal message would find
// the other route open.
func TestGitThroughTheShellIsRefusedForARefusedSubcommand(t *testing.T) {
	shell := NewShell(tree(t))

	for _, subcommand := range []string{"clean", "reset", "filter-branch", "gc", "prune"} {
		t.Run(subcommand, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{
				"command": "git",
				"args":    []string{subcommand},
			}))
			if result.Err == nil {
				t.Fatalf("git %s through the shell ran, want it refused", subcommand)
			}
			if !errors.Is(result.Err, ErrRefusedGit) {
				t.Errorf("the failure is %v, want it to carry ErrRefusedGit", result.Err)
			}
		})
	}
}

// TestGitThroughTheShellIsRefusedForAnUnpermittedSubcommand covers the other refusal,
// since a subcommand the git tool does not run must not run this way either.
func TestGitThroughTheShellIsRefusedForAnUnpermittedSubcommand(t *testing.T) {
	shell := NewShell(tree(t))

	result := invoke(shell, body(t, map[string]any{
		"command": "git",
		"args":    []string{"not-a-subcommand"},
	}))
	if result.Err == nil {
		t.Fatal("an unpermitted subcommand ran through the shell, want it refused")
	}
	if !strings.Contains(result.Err.Error(), "through the shell") {
		t.Errorf("the failure is %q, want it to name the route", result.Err)
	}
}

// TestGitThroughTheShellAcceptsAPermittedSubcommand covers the other direction, so the
// refusals cannot pass by refusing every git call.
func TestGitThroughTheShellAcceptsAPermittedSubcommand(t *testing.T) {
	for _, subcommand := range []string{"status", "log", "diff", "add", "commit"} {
		t.Run(subcommand, func(t *testing.T) {
			if err := gitThroughShell([]string{subcommand}); err != nil {
				t.Errorf("git %s through the shell was refused: %v", subcommand, err)
			}
		})
	}
}

// TestGitThroughTheShellRefusesTheRedirectingOptions covers the options that point git
// at another repository.
//
// A command pointed elsewhere is not a command about the working directory this tool
// contains, and -C is the shortest way to say so.
func TestGitThroughTheShellRefusesTheRedirectingOptions(t *testing.T) {
	shell := NewShell(tree(t))

	for _, option := range []string{"-C", "--git-dir", "--work-tree", "--exec-path", "--all"} {
		t.Run(option, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{
				"command": "git",
				"args":    []string{"status", option, "/elsewhere"},
			}))
			if result.Err == nil {
				t.Errorf("git status %s ran, want it refused", option)
			}
		})
	}
}

// TestGitThroughTheShellRefusesAProgramOptionAsASubcommand covers the shape of a call
// whose first argument is an option.
//
// git -C somewhere status is a command with a global option in front of it. There is no
// subcommand to judge there, and the option is refused on its own account.
func TestGitThroughTheShellRefusesAProgramOptionAsASubcommand(t *testing.T) {
	if err := gitThroughShell([]string{"--version"}); err != nil {
		t.Errorf("git --version was refused as a subcommand: %v", err)
	}
}

// TestGitThroughTheShellRefusesAMarkerInAnOption covers that an option is judged as an
// option.
//
// The markers are for arguments a program will interpret, and an option is named rather
// than interpreted.
func TestGitThroughTheShellRefusesAMarkerInAnOption(t *testing.T) {
	if err := gitThroughShell([]string{"log", "--grep=system("}); err != nil {
		t.Errorf("a marker inside an option was refused: %v", err)
	}
}

// TestTheMarkersStopAProgramRunningSomething covers the gap the shape table cannot see.
//
// An argument that makes a program run a command is not a path, so the path checks pass it
// through untouched. That is what this list is for.
func TestTheMarkersStopAProgramRunningSomething(t *testing.T) {
	shell := NewShell(tree(t))

	cases := []struct {
		program string
		arg     string
	}{
		{"awk", `{ system("id") }`},
		{"awk", `BEGIN { system ("rm -rf /") }`},
		{"sed", `s/a/b/w /etc/passwd`},
		{"sed", "s/a/b/e"},
	}

	for _, tc := range cases {
		t.Run(tc.program+" "+tc.arg, func(t *testing.T) {
			result := invoke(shell, body(t, map[string]any{
				"command": tc.program,
				"args":    []string{tc.arg},
			}))
			if result.Err == nil {
				t.Errorf("%s %q ran, want it refused", tc.program, tc.arg)
			}
		})
	}
}

// TestTheMarkersLeaveAnOrdinaryArgumentAlone covers the other direction, so the
// refusals cannot pass by refusing everything.
//
// An ordinary sed script and an ordinary awk program contain none of the markers, and a
// model rewriting text should be able to. The check is on the arguments rather than on
// the exit status, since a sed or awk that found nothing is a program that worked.
func TestTheMarkersLeaveAnOrdinaryArgumentAlone(t *testing.T) {
	shell := NewShell(tree(t))

	cases := []struct {
		program string
		args    []string
	}{
		{"sed", []string{"s/old/new/", "one.txt"}},
		{"sed", []string{"-n", "s/^/>/p", "one.txt"}},
		{"awk", []string{"{ print $1 }", "one.txt"}},
		{"awk", []string{"BEGIN { print \"hello\" }"}},
	}

	for _, tc := range cases {
		t.Run(tc.program+" "+strings.Join(tc.args, " "), func(t *testing.T) {
			if _, err := shell.checkArgs(tc.program, tc.args); err != nil {
				t.Errorf("an ordinary %s call was refused: %v", tc.program, err)
			}
		})
	}
}

// TestTheMarkersArePerProgram covers the scoping.
//
// A global list would refuse && for every program, which breaks grep for a model matching
// text that happens to contain two characters. awk is the only program on the list that
// interprets its own arguments.
//
// The check is on the argument check rather than on the program running, since grep
// exits non-zero when it matches nothing and a refusal and a search that found nothing
// would otherwise look the same.
func TestTheMarkersArePerProgram(t *testing.T) {
	shell := NewShell(tree(t))

	// A pattern carrying two characters that a global list would have refused.
	if _, err := shell.checkArgs("grep", []string{"a && b", "one.txt"}); err != nil {
		t.Errorf("grep with && in a pattern was refused: %v", err)
	}

	// And neither was echo, which prints what it is given.
	if _, err := shell.checkArgs("echo", []string{"a && b"}); err != nil {
		t.Errorf("echo with && was refused: %v", err)
	}

	// The same pattern to awk is refused, since awk will interpret it.
	if _, err := shell.checkArgs("awk", []string{`{ print "a && b" }`}); err != nil {
		t.Errorf("awk with && in a program was refused, want it refused as a marker: %v", err)
	}
}

// TestAMarkerIsRefusedOnlyInTheArgumentItWouldInterpret covers the option carve out.
//
// sed -i writes, and it is refused as an option. A flag that merely carries the letters
// of a marker is still a flag, and is judged as one.
func TestAMarkerIsRefusedOnlyInTheArgumentItWouldInterpret(t *testing.T) {
	shell := NewShell(tree(t))

	// sed -i is in the marker list, and it is refused.
	result := invoke(shell, body(t, map[string]any{
		"command": "sed",
		"args":    []string{"-i", "s/a/b/", "one.txt"},
	}))
	if result.Err == nil {
		t.Error("sed -i ran, want it refused: it writes the file it edits")
	}

	// A flag carrying a marker is still a flag.
	if _, refused := isRefusedMarker("awk", "-fsystem("); refused {
		t.Error("a flag carrying a marker was refused, want options judged as options")
	}
}
