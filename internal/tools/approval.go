package tools

import (
	"fmt"
	"strings"
)

// ErrApproval is the sentinel every refusal of this package carries.
//
// It is a type rather than a bare error so that errors.Is matches it while errors.As
// still reaches an ApprovalError carried inside a Result. A caller needs both: it needs
// to know a call was refused, and it needs to know whether the refusal was about what
// the call asked for or about the session the reader has set up.
var ErrApproval = approvalSentinel{}

type approvalSentinel struct{}

func (approvalSentinel) Error() string { return "tools: refused" }

// Is reports whether a target is this sentinel, so errors.Is matches a value of any
// of its forms.
func (approvalSentinel) Is(target error) bool {
	_, ok := target.(approvalSentinel)
	return ok
}

// ErrRefused is returned when a call is refused rather than failing.
//
// It is a refusal and not a fault: a model asked for something it may not have is told
// so and carries on, which is different from a program that ran and failed.
var ErrRefused = ErrApproval

// ApprovalError is returned when a call is refused because of the approval mode
// rather than because of what it asked for.
//
// It is a named type so the two refusals can be told apart. A call refused because it
// would leave the tree is a fact about the call, and asking the reader about it would
// be asking about something that cannot be allowed. A call refused because the mode
// is deny is a fact about the session, and the reader can change it.
type ApprovalError struct {
	// Program is the program that would have run.
	Program string

	// Mode is the mode that refused it, or empty when the mode was not set.
	Mode string

	// Reason is what the refusal says.
	Reason string
}

// Error implements error.
func (e *ApprovalError) Error() string {
	if e.Mode == "" {
		return fmt.Sprintf("tools: %s is refused: %s", e.Program, e.Reason)
	}
	return fmt.Sprintf("tools: %s is refused, and the approval mode is %s: %s",
		e.Program, e.Mode, e.Reason)
}

// Unwrap lets errors.Is reach the sentinel through an ApprovalError, so a caller can
// ask whether a result was refused without knowing which kind.
func (e *ApprovalError) Unwrap() error { return ErrApproval }

// The three approval modes, carried here so the tools package can name them without
// importing the config package.
//
// The dependency direction is one-way: main imports config and tools, and tools does
// not import config. So the vocabulary is spelled out on both sides rather than shared
// through a third package that exists only to hold three strings, and a test in each
// package checks its own spelling against the other.
const (
	// ApprovalAsk puts every call to the reader.
	ApprovalAsk = "ask"

	// ApprovalAllow runs every call without asking. It widens nothing.
	ApprovalAllow = "allow"

	// ApprovalDeny refuses every call without asking.
	ApprovalDeny = "deny"
)

// ApprovalModes is the set of modes, in the order they are offered.
var ApprovalModes = []string{ApprovalAsk, ApprovalAllow, ApprovalDeny}

// parseApproval reads a mode, and reports one that is not a mode.
//
// It is the same reading the config package does, and the two are held together by a
// test rather than by a shared import: one package holding three strings is not worth a
// dependency edge that runs the wrong way.
func parseApproval(s string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(s))
	switch mode {
	case "":
		return ApprovalAsk, nil
	case ApprovalAsk, ApprovalAllow, ApprovalDeny:
		return mode, nil
	default:
		return "", fmt.Errorf("%s is not one of %s", s, strings.Join(ApprovalModes, ", "))
	}
}

// approvalModeNames renders the modes for a refusal.
func approvalModeNames() string { return strings.Join(ApprovalModes, ", ") }

// cutWildcard splits an argument at the first wildcard it carries.
//
// The head is the directory leading to the wildcard and the pattern is the argument
// from the wildcard on. A wildcard at index zero yields an empty head, which resolves
// to the working directory, so a pattern with no directory in front of it is judged as
// the directory it will be expanded in rather than refused. That is documented
// behaviour and not an oversight: it is the ordinary form a shell would expand, and
// refusing it would refuse most of what a model asks for.
func cutWildcard(arg string) (head, pattern string, found bool) {
	i := strings.IndexAny(arg, "*?[")
	if i < 0 {
		return "", "", false
	}
	return arg[:i], arg[i:], true
}

// checksWildcard judges a path argument carrying a wildcard and reports what the
// program should be given.
//
// An argument with no wildcard is not this function's business: it is handed back
// unchanged so the caller can resolve it as an ordinary path. That distinction matters,
// because an argument passed through unchecked here would skip the containment test
// entirely, and an absolute path would go straight to the program.
//
// A pattern is returned unchanged when it is acceptable, since no shell is read and the
// program is what expands it.
func checksWildcard(root, arg string) (string, error) {
	head, pattern, found := cutWildcard(arg)
	if !found {
		return arg, nil
	}

	// The directory leading to the wildcard is the thing being judged, since that is
	// the directory the pattern will be expanded against. An empty head is the working
	// directory, which the caller is already in.
	if _, err := containment(root, head); err != nil {
		return "", fmt.Errorf("%w: the pattern %q is judged by %q, which leaves the tree",
			ErrRefused, arg, head)
	}
	return pattern, nil
}
