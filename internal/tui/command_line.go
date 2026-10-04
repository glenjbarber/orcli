package tui

import "strings"

// IsCommand reports whether a typed line is a command, and returns the name and the
// arguments.
//
// It is in this package beside Lookup because it is the same question the table
// answers: what did the reader type, and does it name something. A dispatcher in
// main asks it before it asks anything else, and a dispatcher that split the line
// itself would be a second spelling of the same split that could disagree with the
// completer's.
//
// The name arrives without the slash, the same way Lookup wants it, so the caller
// passes what it gets straight through rather than stripping a second time.
//
// A line with no slash is not a command, and it is not an error either: it is the
// ordinary case, a question the reader wants to ask. A line that is only a slash is
// not a command, since a name of nothing is a name the table does not have and
// reporting it as one would send a reader looking for it.
func IsCommand(line string) (name, args string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}

	name, args, _ = strings.Cut(strings.TrimPrefix(trimmed, "/"), " ")
	if name == "" {
		return "", "", false
	}
	return name, strings.TrimSpace(args), true
}
