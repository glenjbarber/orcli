package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pointingAt makes lookPath resolve only into dir, for the length of the test.
//
// The shell and git tools resolve a program by bare name, so a test that wants a
// program of its own has to put it where the resolution will find it and nothing else.
// Restoring the field matters: it is a package variable, and a test that left it
// pointing at a temporary directory would hand every later test an empty PATH.
func pointingAt(t *testing.T, dir string) {
	t.Helper()

	original := lookPath
	t.Cleanup(func() { lookPath = original })
	lookPath = func(name string) (string, error) {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err != nil {
			return "", errors.New("executable file not found in $PATH")
		}
		return candidate, nil
	}
}

// TestRunProgramKeepsOutputUnderTheCap is the other half of the cap, so the bound
// cannot pass by refusing everything.
//
// bounds_test.go holds the over-the-cap direction and the boundary. This covers the
// ordinary case: a program that writes a little succeeds and its output arrives whole.
func TestRunProgramKeepsOutputUnderTheCap(t *testing.T) {
	path := standIn(t, "chatty", "echo small output\n")

	out, err := runProgram(path, nil, t.TempDir(), time.Minute)
	if err != nil {
		t.Fatalf("a program under the cap failed: %v", err)
	}
	if got, want := strings.TrimSpace(string(out)), "small output"; got != want {
		t.Errorf("the output is %q, want %q", got, want)
	}
}

// TestAnUnlistedProgramIsStillRefusedWithTheBoundsInPlace checks that the bounds did
// not open a route past the allowlist.
//
// The timeout and the cap are applied where a program runs, so the allowlist check is
// what stands between a model and a program of its own choosing. A stand-in that is
// not on the list must still be refused by name before anything runs, and this is the
// test that would fail if a future change moved the deadline ahead of the list check.
func TestAnUnlistedProgramIsStillRefusedWithTheBoundsInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sleeper")
	script := "#!/bin/sh\necho started; sleep 30\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	pointingAt(t, dir)

	shell := NewShell(tree(t))
	shell.Dir = dir
	shell.Approval = "allow"

	result := invoke(shell, body(t, map[string]any{"command": "sleeper"}))
	if result.Err == nil {
		t.Fatal("a program that is not on the allowlist ran, want it refused")
	}
	if !errors.Is(result.Err, ErrRefused) {
		t.Errorf("the refusal is %v, want it to carry ErrRefused", result.Err)
	}
}
