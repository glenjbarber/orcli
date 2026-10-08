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
// Four outcomes are distinguished, because they are four different things for a
// reader to be told. A file carrying a credential is nil. A file carrying none
// is ErrNoAPIKey, along with the configuration that was read, because an
// interface that refuses to open leaves nothing on screen explaining why. No
// file at all is ErrNotFound, which is first-time setup rather than a fault.
// Everything else is fatal: a bad mode, a malformed body, a top level that is
// not an object, or a path that is a directory.
//
// There are exactly four writers, and no others:
//
//	InstallDefault   startup, only when absent, exclusive create, 0600
//	EnsureAPIKeyStub adds an empty api_key member when it is absent
//	Trust            adds a directory to ORCLI_TRUSTED
//	WriteColor       sets the top-level color key
//
// Neither runtime writer ever creates the file. A file made by a command would
// hold no credential and would suppress first-time setup, which is worse than
// the absence it was meant to remedy.
//
// The members this package names are its own: ORCLI_TRUSTED, ORCLI_READABLE and
// the cloudflare block, beside the ordinary lowercase settings. The record of
// what this client was allowed to do in a directory is a fact about this
// program, not about who answers a request, so it is spelled with this
// program's own prefix.
package config
