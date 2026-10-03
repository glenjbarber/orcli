package tools

import "strings"

// isOption reports whether an argument is an option rather than a path.
//
// A dash is what separates the two, and an argument carrying one is read as the flag
// it is however much it resembles a path. -o is not a path and -2024-01-01 is not a
// path either, and treating either as one would refuse ordinary calls.
//
// It lives here rather than in either tool because both need it and neither owns it:
// deciding whether an argument is a path is a question about the argument, while which
// arguments are paths is a question about the program.
func isOption(arg string) bool { return strings.HasPrefix(arg, "-") }
