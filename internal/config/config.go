// Package config reads and writes the orcli configuration file.
//
// The file is JSON, because the standard library has no YAML parser and a
// configuration file is not a place where a dependency is worth taking. The
// accepted cost is that JSON permits no comments and demands strict punctuation.
//
// The file holds a credential, which shapes almost everything here. It must be
// `0600` and any other mode is a hard failure rather than a warning, because a
// permissive mode leaks the credential silently and the reader has no reason to
// go looking for it. The credential is never read from the process
// environment: an environment variable of the same name is ignored even when
// set, which removes the class of failure in which a correct file is shadowed
// by a stale value somewhere else.
//
// There are exactly three writers, and no others:
//
//	InstallDefault   startup, only when absent, exclusive create, 0600
//	Trust            adds a directory to OPENROUTER_TRUSTED
//	WriteColor       sets the top-level color key
//
// Neither runtime writer ever creates the file. A file made by a command would
// hold no credential and would suppress first-time setup, which is worse than
// the absence it was meant to remedy.
package config
