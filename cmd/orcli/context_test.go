package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestContextReportsNoneReadBeforeAnyIntroduction covers the state /context starts
// in: nothing has been read yet, and it says so rather than printing an empty list.
func TestContextReportsNoneReadBeforeAnyIntroduction(t *testing.T) {
	resetGlobalContextLog()
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/context")
	if err != nil {
		t.Fatalf("/context: %v", err)
	}
	if out.Text != "no context files have been read yet" {
		t.Errorf("/context = %q, want the no-files message", out.Text)
	}
}

// TestContextListsAGENTSMDAfterIntroduction covers the path this feature exists for:
// once introduction() has read an AGENTS.md, /context names its absolute path, so a
// reader can check what actually grounded a turn instead of assuming.
func TestContextListsAGENTSMDAfterIntroduction(t *testing.T) {
	resetGlobalContextLog()
	d := togglesDispatcher(t)

	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# intro"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := introduction(dir); got != "# intro" {
		t.Fatalf("introduction(%q) = %q, want the file's own text", dir, got)
	}

	out, err := d.Run(context.Background(), "/context")
	if err != nil {
		t.Fatalf("/context: %v", err)
	}
	absAgentsPath, err := filepath.Abs(agentsPath)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if !strings.Contains(out.Text, absAgentsPath) {
		t.Errorf("/context = %q, want it to name %s", out.Text, absAgentsPath)
	}
}

// TestContextDoesNotDuplicateARepeatedRead covers that reading the same AGENTS.md
// twice, which introduction() does once per ask built over the same directory, lists
// that one path once rather than twice.
func TestContextDoesNotDuplicateARepeatedRead(t *testing.T) {
	resetGlobalContextLog()
	d := togglesDispatcher(t)

	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# intro"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	introduction(dir)
	introduction(dir)

	out, err := d.Run(context.Background(), "/context")
	if err != nil {
		t.Fatalf("/context: %v", err)
	}
	if n := strings.Count(out.Text, "AGENTS.md"); n != 1 {
		t.Errorf("/context names AGENTS.md %d times, want 1:\n%s", n, out.Text)
	}
}

// TestContextRefusesArguments covers that /context, like /version and /clear, takes
// no argument, so a reader who types one is told rather than silently ignored.
func TestContextRefusesArguments(t *testing.T) {
	resetGlobalContextLog()
	d := togglesDispatcher(t)

	if _, err := d.Run(context.Background(), "/context extra"); err == nil {
		t.Error("/context extra: want an error, got nil")
	}
}

// TestContextMissingAGENTSMDIsNotRecorded covers that a missing AGENTS.md, which
// introduction() already treats as silence rather than a failure, leaves nothing for
// /context to report either.
func TestContextMissingAGENTSMDIsNotRecorded(t *testing.T) {
	resetGlobalContextLog()
	d := togglesDispatcher(t)

	introduction(t.TempDir())

	out, err := d.Run(context.Background(), "/context")
	if err != nil {
		t.Fatalf("/context: %v", err)
	}
	if out.Text != "no context files have been read yet" {
		t.Errorf("/context = %q, want the no-files message", out.Text)
	}
}
