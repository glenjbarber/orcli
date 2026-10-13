// Package keychain resolves a secret from the macOS Keychain by shelling out
// to the `security` command line tool, as issue #67's own design question
// settles it: the Security framework's native API has no stable, unprivileged
// cgo-free binding this project can reach without adding a cgo dependency to
// a client that otherwise has none, and `security find-generic-password`
// already does exactly the lookup this package needs.
//
// Every call here goes through exec.CommandContext with an argument array,
// never a shell - a configured account or service name reaches argv as a
// single element, never as text interpolated into a command line, so a name
// containing a quote or a semicolon is passed as-is rather than parsed as
// shell syntax.
//
// The one invariant every function here is built to keep: a secret value
// never appears in a returned error. Lookup separates the command's stdout
// (the secret) from its stderr (the diagnostic), and only stderr - plus the
// account/service the caller supplied, which is a reference, not a credential
// - is allowed into an error. Set and Delete never see the secret's bytes
// again once it reaches argv, so they have nothing to leak by construction.
package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Ref names one Keychain item: the account and service (item name) pair
// `security find-generic-password`'s own -a and -s flags match against.
// Neither is a secret - both are the reference a configuration file is
// meant to carry instead of the credential itself (issue #67).
type Ref struct {
	Account string
	Service string
}

// Empty reports whether r names no item at all, as opposed to one this
// Keychain happens not to have - a caller checks this before Lookup to tell
// "not configured" from "configured and missing" apart, which need different
// messages.
func (r Ref) Empty() bool {
	return r.Account == "" && r.Service == ""
}

func (r Ref) String() string {
	return fmt.Sprintf("account=%q service=%q", r.Account, r.Service)
}

// ErrUnsupported is returned on any OS but macOS. `security` is a macOS-only
// binary, and a clear refusal here is more useful than the "executable file
// not found in $PATH" a bare exec would produce.
var ErrUnsupported = errors.New("keychain: only supported on macOS")

// ErrNotFound is returned when the Keychain has no item matching a Ref.
var ErrNotFound = errors.New("keychain: no matching item")

// ErrIncompleteRef is returned when a Ref is missing its account or its
// service - find-generic-password needs both to be a narrow, correct match
// rather than the first same-service item it happens to find.
var ErrIncompleteRef = errors.New("keychain: a reference needs both an account and a service")

// goos is runtime.GOOS, held as a variable so a test can exercise the
// unsupported-platform path on whatever OS actually runs the test suite
// (this repository's own CI runs on Linux, never macOS).
var goos = runtime.GOOS

// runner is the subprocess seam every exported function goes through, and
// the one a test replaces. It is a function type rather than an interface
// because every call here has the same shape: a program name, its
// arguments, and stdout/stderr returned separately so a caller can decide
// which of the two is safe to put in an error.
type runner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

// run is the real subprocess runner. Replaced in tests with one that never
// touches an actual Keychain.
var run runner = execRun

func execRun(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// securityNotFoundExitCode is `security`'s own exit status for
// errSecItemNotFound (-25300), documented in Apple's Security framework
// headers and the status find-generic-password/delete-generic-password both
// return for no match.
const securityNotFoundExitCode = 44

// isNotFound reports whether err is security's own "no such item" outcome,
// checked by exit code first (the stable signal across macOS releases) and
// by the one stderr phrase security has used for this outcome as a fallback,
// in case a future release changes the exit code but not the wording.
func isNotFound(err error, stderr []byte) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == securityNotFoundExitCode {
		return true
	}
	return bytes.Contains(stderr, []byte("could not be found in the keychain"))
}

// commandErr builds the error Lookup/Set/Delete return for a failed
// command. It is handed the reference and stderr, and explicitly NOT the
// command's stdout - the one place a secret could otherwise leak through is
// a caller passing stdout in here by mistake, and this signature makes that
// impossible rather than merely documented against.
func commandErr(verb string, ref Ref, stderr []byte, err error) error {
	detail := strings.TrimSpace(string(stderr))
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("keychain: %s %s: %s", verb, ref, detail)
}

// Lookup reads the secret Ref names.
//
// The secret is returned as the function's own value, never embedded in an
// error - a caller that logs an error from this function cannot log a
// secret by doing so, because the secret is never in one.
func Lookup(ctx context.Context, ref Ref) (string, error) {
	if goos != "darwin" {
		return "", ErrUnsupported
	}
	if ref.Account == "" || ref.Service == "" {
		return "", fmt.Errorf("%w (got %s)", ErrIncompleteRef, ref)
	}
	stdout, stderr, err := run(ctx, "security", "find-generic-password",
		"-a", ref.Account, "-s", ref.Service, "-w")
	if err != nil {
		if isNotFound(err, stderr) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
		}
		return "", commandErr("looking up", ref, stderr, err)
	}
	// `-w` prints the secret followed by exactly one trailing newline;
	// TrimRight rather than TrimSpace, since a secret itself may legitimately
	// carry leading or trailing spaces that are not this package's to guess
	// are insignificant.
	return strings.TrimRight(string(stdout), "\n"), nil
}

// Set writes secret under ref, creating the item if none exists and
// updating it in place (`-U`) if one already does - the same
// create-or-update shape this project's own config writers use, so "set a
// credential" is one operation rather than two the caller has to choose
// between.
//
// secret reaches the `security` process as one argv element, which is the
// documented limitation of driving this particular CLI rather than the
// Security framework's own API directly (see this package's own doc
// comment): it is visible to another local process briefly through that
// process's own command line, the same exposure every other tool built on
// this CLI (including Apple's own documentation for it) carries. It is
// never written to this process's own logs, stdout, or any error this
// package returns.
func Set(ctx context.Context, ref Ref, secret string) error {
	if goos != "darwin" {
		return ErrUnsupported
	}
	if ref.Account == "" || ref.Service == "" {
		return fmt.Errorf("%w (got %s)", ErrIncompleteRef, ref)
	}
	_, stderr, err := run(ctx, "security", "add-generic-password",
		"-a", ref.Account, "-s", ref.Service, "-w", secret, "-U")
	if err != nil {
		return commandErr("writing", ref, stderr, err)
	}
	return nil
}

// Delete removes the item Ref names. Only that one item: `security
// delete-generic-password` matches on the same -a/-s pair Lookup and Set
// use, so a Delete for one named credential cannot reach a different one
// that happens to share only an account or only a service with it.
func Delete(ctx context.Context, ref Ref) error {
	if goos != "darwin" {
		return ErrUnsupported
	}
	if ref.Account == "" || ref.Service == "" {
		return fmt.Errorf("%w (got %s)", ErrIncompleteRef, ref)
	}
	_, stderr, err := run(ctx, "security", "delete-generic-password",
		"-a", ref.Account, "-s", ref.Service)
	if err != nil {
		if isNotFound(err, stderr) {
			return fmt.Errorf("%w: %s", ErrNotFound, ref)
		}
		return commandErr("deleting", ref, stderr, err)
	}
	return nil
}
