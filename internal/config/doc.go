// Package config reads and writes the configuration file.
//
// It is JSON, because the standard library has no YAML parser and a
// configuration file is not a place where a dependency is worth taking. The
// accepted cost is that JSON permits no comments and demands strict
// punctuation.
//
// The file holds a credential, so two things follow from that and shape the
// whole package. It must be `0600`, because a permissive mode leaks the key
// silently and no warning is shown for a credential that has already leaked.
// And there are exactly three writers, named in this package and no others,
// because a file holding a credential rewritten by ad hoc code is a file whose
// contents nobody can account for.
//
// # Environment
//
// No environment variable is read by this package, with one exception. PATH is
// consulted, because the shell tool resolves programs by bare name through it
// and there is no other way to find them. Every other variable is ignored,
// including any whose name matches a key.
//
// OPENROUTER_API_KEY is named here because it is the one that matters. An
// environment variable of that name is ignored even when set. This removes the
// class of failure in which a correct file is shadowed by a stale value
// elsewhere, and it removes the class of failure in which a diagnostic names an
// environment variable and sends a reader to edit their shell profile instead
// of the file that is actually read. A key lives in one place or it is not a
// key, it is an ambiguity.
package config
