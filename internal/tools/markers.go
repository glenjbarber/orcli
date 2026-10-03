package tools

import (
	"strings"
)

// programMarkers are the fragments a program may not be given in an argument.
//
// The path shape table answers whether an argument is a path. This answers a
// different question: whether the argument would make the program act outside its own
// work. Those arguments are not paths, so no path check can see them, and the shape
// table is right to pass them through untouched. That is the gap this closes.
//
// The markers are per program rather than global, and that is the whole reason this
// works. A global list refused && and || for everything, which broke
// `grep "a && b" file` for a model matching text that happens to contain two
// characters. awk is the only program on the allowlist that interprets its own
// arguments, and sed is the only one with a script that writes. A model wanting a
// system call should not have one, so refusing system( for awk costs nothing.
var programMarkers = map[string][]string{
	// awk runs a shell from inside a program, which is the one thing an argument
	// array cannot stop. getline can also assign into a variable, which is a
	// program writing to itself, so it is named too.
	"awk": {"system(", "system (", "getline "},

	// sed writes through three of its own commands, none of which is a flag and
	// none of which is a path: w appends the pattern space to a named file, W
	// writes the pattern space without appending, and e runs the pattern space as
	// a command. A redirection inside a substitution is a write as well.
	//
	// This is the more restrictive reading. Refusing "w " costs a sed call whose
	// pattern happens to contain a letter w followed by a space, and a model
	// wanting to match that text is better served by grep. Refusing it costs
	// nothing worth having.
	"sed": {"w ", "W ", "e ", "-i", "--in-place"},
}

// isRefusedMarker reports whether an argument carries a fragment its program may
// not be given, and which one.
//
// The match is on the argument as written, since it is the argument the program
// will read. A case-insensitive match would refuse a pattern containing System,
// which is not what the marker is about.
func isRefusedMarker(program, arg string) (string, bool) {
	markers, known := programMarkers[program]
	if !known {
		return "", false
	}

	// An option is a flag the program was asked for by name. A marker inside one
	// is the program being asked to do something by name, which is a different
	// question from a marker inside an argument it will interpret, and is already
	// answered by the refused-option table.
	if isOption(arg) {
		return "", false
	}

	for _, marker := range markers {
		if strings.Contains(arg, marker) {
			return marker, true
		}
	}
	return "", false
}
