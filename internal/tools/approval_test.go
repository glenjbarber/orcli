package tools

import (
	"errors"
	"strings"
	"testing"
)

// TestTheModeIsConsultedBeforeAnythingRuns covers the wiring that makes the setting
// do something.
//
// A mode that is read from a file and never consulted is a setting that looks like it
// works and does not, which is worse than no setting at all.
func TestTheModeIsConsultedBeforeAnythingRuns(t *testing.T) {
	dir := tree(t)

	// deny refuses every permitted program, including one that is installed and
	// would otherwise run.
	shell := NewShell(dir)
	shell.Approval = ApprovalDeny

	result := invoke(shell, body(t, map[string]any{"command": "ls"}))
	if result.Err == nil {
		t.Fatal("a permitted program ran with the mode set to deny, want it refused")
	}

	var refusal *ApprovalError
	if !errors.As(result.Err, &refusal) {
		t.Fatalf("the failure is %v, want an ApprovalError", result.Err)
	}
	if refusal.Mode != ApprovalDeny {
		t.Errorf("the refusal names the mode %q, want %q", refusal.Mode, ApprovalDeny)
	}
	if refusal.Program != "ls" {
		t.Errorf("the refusal names the program %q, want ls", refusal.Program)
	}
}

// TestDenyIsCheckedAfterTheListAndTheArguments covers the order.
//
// A call that could not have run anyway is not worth refusing on the mode's account,
// so the refusal a reader sees is about the thing that was actually wrong with it.
func TestDenyIsCheckedAfterTheListAndTheArguments(t *testing.T) {
	dir := tree(t)
	shell := NewShell(dir)
	shell.Approval = ApprovalDeny

	// A program outside the list is refused by name, not by the mode.
	result := invoke(shell, body(t, map[string]any{"command": "curl"}))
	if result.Err == nil {
		t.Fatal("the call succeeded, want a refusal")
	}
	if !strings.Contains(result.Err.Error(), "not permitted") {
		t.Errorf("the failure is %q, want it to be about the program rather than the mode",
			result.Err)
	}

	// A path leaving the tree is refused as such, not by the mode.
	result = invoke(shell, body(t, map[string]any{"command": "cat", "args": []string{"../x"}}))
	if result.Err == nil {
		t.Fatal("the call succeeded, want a refusal")
	}
	if !strings.Contains(result.Err.Error(), "outside") {
		t.Errorf("the failure is %q, want it to be about the path rather than the mode",
			result.Err)
	}
}

// TestAnUnsetModeIsRefusedRatherThanDefaulted covers the choice made here.
//
// A session that never read a configuration cannot say what it would do, and
// defaulting to the asking mode would make a missing file look like a deliberate
// choice.
func TestAnUnsetModeIsRefusedRatherThanDefaulted(t *testing.T) {
	shell := NewShell(tree(t))
	shell.Approval = ""

	result := invoke(shell, body(t, map[string]any{"command": "ls"}))
	if result.Err == nil {
		t.Fatal("a call with no mode set succeeded, want a refusal")
	}
	if !strings.Contains(result.Err.Error(), "no approval mode is set") {
		t.Errorf("the failure is %q, want it to say the mode is not set", result.Err)
	}

	g := NewGit(repo(t))
	g.Approval = ""
	if result := invoke(g, body(t, map[string]any{"subcommand": "status"})); result.Err == nil {
		t.Error("a git call with no mode set succeeded, want a refusal")
	}
}

// TestAModeThatIsNotOneOfThreeIsRefused covers a file written by hand.
func TestAModeThatIsNotOneOfThreeIsRefused(t *testing.T) {
	dir := tree(t)

	for _, mode := range []string{"refuse", "permitted", "ALLOW_MAYBE", "1"} {
		shell := NewShell(dir)
		shell.Approval = mode

		result := invoke(shell, body(t, map[string]any{"command": "ls"}))
		if result.Err == nil {
			t.Errorf("the mode %q was accepted, want it refused", mode)
			continue
		}
		if !strings.Contains(result.Err.Error(), mode) {
			t.Errorf("the failure for the mode %q is %q, want it to name the mode",
				mode, result.Err)
		}
	}
}

// TestAskAndAllowBothRun covers the other two modes, so that deny is not passing
// because nothing runs at all.
func TestAskAndAllowBothRun(t *testing.T) {
	dir := tree(t)

	for _, mode := range []string{ApprovalAsk, ApprovalAllow} {
		t.Run(mode, func(t *testing.T) {
			shell := NewShell(dir)
			shell.Approval = mode

			result := invoke(shell, body(t, map[string]any{"command": "ls"}))
			if result.Err != nil {
				t.Errorf("ls with the mode %q was refused: %v", mode, result.Err)
			}
		})
	}
}

// TestGitIsWiredToTheSameMode covers the git tool, which carries its own field.
func TestGitIsWiredToTheSameMode(t *testing.T) {
	r := repo(t)

	g := NewGit(r)
	g.Approval = ApprovalDeny

	result := invoke(g, body(t, map[string]any{"subcommand": "status"}))
	if result.Err == nil {
		t.Fatal("git ran with the mode set to deny, want it refused")
	}

	var refusal *ApprovalError
	if !errors.As(result.Err, &refusal) {
		t.Fatalf("the failure is %v, want an ApprovalError", result.Err)
	}
	if !strings.Contains(refusal.Program, "status") {
		t.Errorf("the refusal names %q, want it to name the subcommand", refusal.Program)
	}

	g.Approval = ApprovalAsk
	if result := invoke(g, body(t, map[string]any{"subcommand": "status"})); result.Err != nil {
		t.Errorf("git status with the mode ask was refused: %v", result.Err)
	}
}

// TestTheNewToolsDefaultToAsk covers the constructors, since a tool built and never
// configured should ask rather than refuse or run.
func TestTheNewToolsDefaultToAsk(t *testing.T) {
	if got := NewShell(tree(t)).Approval; got != ApprovalAsk {
		t.Errorf("a new shell is in the mode %q, want %q", got, ApprovalAsk)
	}
	if got := NewGit(repo(t)).Approval; got != ApprovalAsk {
		t.Errorf("a new git tool is in the mode %q, want %q", got, ApprovalAsk)
	}
}

// TestARefusalCarriesTheSentinel covers both refusals being tellable apart.
func TestARefusalCarriesTheSentinel(t *testing.T) {
	shell := NewShell(tree(t))

	shell.Approval = ApprovalDeny
	if err := invoke(shell, body(t, map[string]any{"command": "ls"})).Err; !errors.Is(err, ErrRefused) {
		t.Errorf("a denied call is %v, want it to carry the refusal sentinel", err)
	}

	shell.Approval = ApprovalAsk
	if err := invoke(shell, body(t, map[string]any{"command": "curl"})).Err; !errors.Is(err, ErrRefused) {
		t.Errorf("a refused program is %v, want it to carry the refusal sentinel", err)
	}
}

// TestTheToolModesAreThreeNamedModes covers the vocabulary the tools package carries.
//
// The config package does not spell these names in a place this package can reach,
// since the dependency direction is one-way and tools must not import config. So the
// three are declared here as well, and this test checks the set is what it claims to
// be rather than a drift toward some other spelling.
func TestTheToolModesAreThreeNamedModes(t *testing.T) {
	if len(ApprovalModes) != 3 {
		t.Fatalf("there are %d modes, want 3", len(ApprovalModes))
	}
	for _, want := range []string{ApprovalAsk, ApprovalAllow, ApprovalDeny} {
		found := false
		for _, mode := range ApprovalModes {
			if mode == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is not in ApprovalModes", want)
		}
		if _, err := parseApproval(want); err != nil {
			t.Errorf("the parser refuses the mode %q: %v", want, err)
		}
	}
}
