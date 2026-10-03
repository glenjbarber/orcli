package config

import (
	"errors"
	"fmt"
	"strings"
)

// Approval is the mode that decides whether a tool runs without asking.
//
// It is a mode and not a per-call answer, and the difference matters: a mode is
// set once for a session and answers every call the file rules do not, while a
// per-call answer settles one call and is forgotten. A grant meant to last is
// a rule in the permissions file, and this is the session preference that the
// rules are layered over.
type Approval string

// The three modes.
//
// deny is named deny rather than refuse, because it is a decision and not a
// description of an outcome. A tool that refused for some other reason did not
// deny anything; this one was told no in advance and never ran.
const (
	// ApprovalAsk puts every call to the reader. This is the default, and it is
	// the only mode that asks.
	ApprovalAsk Approval = "ask"

	// ApprovalAllow runs every call without asking. It widens nothing: a
	// program that is not permitted is still refused by name before this mode
	// is consulted, so allowing says nothing about what may be run and only
	// about what happens to what has been proposed.
	ApprovalAllow Approval = "allow"

	// ApprovalDeny refuses every call without asking. A reader who sets this
	// gets a session with no tools and no questions, which is a legitimate
	// thing to want and is not the same as a reader who is not trusted: trust
	// is about a directory, and this is about a session.
	ApprovalDeny Approval = "deny"
)

// ErrBadApproval is returned when the file names a mode that is not one of the
// three.
//
// It is a fault rather than a fallback. A file written by hand naming a mode
// this client does not know is a file a reader meant something by, and
// substituting a default would silently do something other than what they
// wrote.
var ErrBadApproval = errors.New("config: the approval mode must be ask, allow, or deny")

// ApprovalModes is the set of modes, in the order they are offered to a reader.
//
// The order is ask, allow, deny rather than the order of how much they permit,
// since a reader choosing between them is answering a question about what they
// want and the safest option belongs first.
var ApprovalModes = []Approval{ApprovalAsk, ApprovalAllow, ApprovalDeny}

// parseApproval reads a mode out of the configuration file.
//
// An empty value is ask, since a file that does not mention the mode is a file
// written before the mode existed, and asking is what it did then.
func parseApproval(s string) (Approval, error) {
	switch Approval(strings.ToLower(strings.TrimSpace(s))) {
	case "":
		return ApprovalAsk, nil
	case ApprovalAsk:
		return ApprovalAsk, nil
	case ApprovalAllow:
		return ApprovalAllow, nil
	case ApprovalDeny:
		return ApprovalDeny, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrBadApproval, s)
	}
}

// Records reports whether a mode is one of the three.
//
// It is exported because a caller that has a mode from somewhere other than the
// file, such as a command-line argument, has to check it the same way rather
// than assuming.
func (a Approval) Records() bool {
	switch a {
	case ApprovalAsk, ApprovalAllow, ApprovalDeny:
		return true
	default:
		return false
	}
}

// String renders the mode as it is written in the file.
func (a Approval) String() string { return string(a) }
