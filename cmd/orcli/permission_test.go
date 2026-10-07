package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// overApproveAt points configPath at a file and returns a dispatcher over a session,
// the way overModelAt wires /model's own seam.
func overApproveAt(t *testing.T, path string) (*dispatcher, *tui.Session) {
	t.Helper()

	old := configPath
	configPath = func() (string, error) { return path, nil }
	t.Cleanup(func() { configPath = old })

	s := tui.New(tui.Options{APIKey: "k"})
	d := newDispatcherFor(config.Default())
	d.withSession(s)
	return d, s
}

// TestApproveWithNoArgumentReportsTheMode covers the reader who has never typed
// /approve, the way /level with no argument reports before any preset is set.
func TestApproveWithNoArgumentReportsTheMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"k"}`), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d, _ := overApproveAt(t, path)

	out, err := d.Run(context.Background(), "/approve")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "no approval mode is set") {
		t.Errorf("the report is %q, want it to say none is set", out.Text)
	}
	for _, want := range []string{"ask", "allow", "deny"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the report does not list %q:\n%s", want, out.Text)
		}
	}
}

// TestApproveSetsTheModeInTheSessionAndTheFile covers the write and the read-back:
// a mode /approve sets has to change both the session a turn is sent with and the
// file a restart reads, the way /model's own test covers the pair.
func TestApproveSetsTheModeInTheSessionAndTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"k"}`), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d, s := overApproveAt(t, path)

	out, err := d.Run(context.Background(), "/approve allow")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "allow") {
		t.Errorf("the reply is %q, want it to name the mode", out.Text)
	}
	if got := s.Options().Approval; got != tui.ApprovalAllow {
		t.Errorf("s.Options().Approval is %q, want %q", got, tui.ApprovalAllow)
	}

	cfg, err := config.Parse(mustRead(t, path), path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mode, err := cfg.ApprovalMode()
	if err != nil {
		t.Fatalf("ApprovalMode: %v", err)
	}
	if mode != config.ApprovalAllow {
		t.Errorf("the file holds %q, want %q", mode, config.ApprovalAllow)
	}

	out, err = d.Run(context.Background(), "/approve")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "allow") {
		t.Errorf("/approve with no argument did not report the mode: %q", out.Text)
	}
}

// TestApproveRefuseIsPointedAtDenyByName covers the word the command table's own
// summary ("ask|allow|refuse") suggests, which is not a mode any part of this tree
// recognises: the real third mode is deny, and a reader who typed what the summary
// implies should be told what to type instead, not handed the bare list every other
// typo gets.
func TestApproveRefuseIsPointedAtDenyByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"k"}`), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d, _ := overApproveAt(t, path)

	_, err := d.Run(context.Background(), "/approve refuse")
	if err == nil {
		t.Fatal("/approve refuse was accepted")
	}
	if !strings.Contains(err.Error(), "deny") {
		t.Errorf("the refusal is %q, want it to name deny", err)
	}
}

// TestApproveWithAnUnknownWordIsRefusedByName covers the ordinary typo.
func TestApproveWithAnUnknownWordIsRefusedByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"k"}`), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d, _ := overApproveAt(t, path)

	_, err := d.Run(context.Background(), "/approve nosuch")
	if err == nil {
		t.Fatal("an unknown mode was accepted")
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("the refusal is %q, want it to name what was typed", err)
	}
}

// TestPermissionWithNoArgumentReportsNothingGranted covers the reader who has never
// typed /permission.
func TestPermissionWithNoArgumentReportsNothingGranted(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	out, err := d.Run(context.Background(), "/permission")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "no permissions are granted") {
		t.Errorf("the report is %q, want it to say none are granted", out.Text)
	}
}

// TestPermissionAddGrantsAndIsReported covers the write and the read-back of the
// in-memory list, since a grant that could not be reported would be one the reader
// cannot confirm took.
func TestPermissionAddGrantsAndIsReported(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	out, err := d.Run(context.Background(), "/permission add /work/repo git make")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "git, make") {
		t.Errorf("the reply is %q, want it to name both programs", out.Text)
	}

	out, err = d.Run(context.Background(), "/permission")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "/work/repo") || !strings.Contains(out.Text, "git") || !strings.Contains(out.Text, "make") {
		t.Errorf("the report does not list the grant:\n%s", out.Text)
	}
}

// TestPermissionRemoveRefusesAProgramAndDropsAnEmptyDirectory covers the two things
// remove has to do: take the named program out of the set, and drop the directory
// entirely once nothing is left granted in it, so a later report does not list a
// directory that grants nothing.
func TestPermissionRemoveRefusesAProgramAndDropsAnEmptyDirectory(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	if _, err := d.Run(context.Background(), "/permission add /work/repo git make"); err != nil {
		t.Fatalf("Run add: %v", err)
	}

	out, err := d.Run(context.Background(), "/permission remove /work/repo make")
	if err != nil {
		t.Fatalf("Run remove: %v", err)
	}
	if !strings.Contains(out.Text, "make") {
		t.Errorf("the reply is %q, want it to name the removed program", out.Text)
	}

	out, err = d.Run(context.Background(), "/permission")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.Text, "make") {
		t.Errorf("make is still reported as granted:\n%s", out.Text)
	}
	if !strings.Contains(out.Text, "git") {
		t.Errorf("git is no longer reported as granted:\n%s", out.Text)
	}

	if _, err := d.Run(context.Background(), "/permission remove /work/repo git"); err != nil {
		t.Fatalf("Run remove: %v", err)
	}
	out, err = d.Run(context.Background(), "/permission")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "no permissions are granted") {
		t.Errorf("an empty directory is still reported:\n%s", out.Text)
	}
}

// TestPermissionAddRequiresADirectoryAndAProgram covers the missing-argument case
// for both halves of "DIR PROG...".
func TestPermissionAddRequiresADirectoryAndAProgram(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	if _, err := d.Run(context.Background(), "/permission add /work/repo"); err == nil {
		t.Error("/permission add with no program was accepted")
	}
	if _, err := d.Run(context.Background(), "/permission add"); err == nil {
		t.Error("/permission add with nothing at all was accepted")
	}
}

// TestPermissionUnknownSubCommandIsRefusedByName covers a typo of add/remove.
func TestPermissionUnknownSubCommandIsRefusedByName(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	_, err := d.Run(context.Background(), "/permission list")
	if err == nil {
		t.Fatal("an unknown sub-command was accepted")
	}
	if !strings.Contains(err.Error(), "list") {
		t.Errorf("the refusal is %q, want it to name what was typed", err)
	}
}

// TestToolsNeedsAnOpenInterface covers the dispatcher with no session, the same
// guard /model and /level each carry.
func TestToolsNeedsAnOpenInterface(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	if _, err := d.Run(context.Background(), "/tools"); err == nil {
		t.Fatal("/tools ran with no session")
	}
}

// TestToolsListsEveryToolAndTheRoot covers the point of the command: a reader asking
// what a turn may call gets the same names and descriptions ask.go's own
// capabilities message would send, plus the directory they are contained to.
func TestToolsListsEveryToolAndTheRoot(t *testing.T) {
	dir := t.TempDir()

	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})
	d.withSession(tui.New(tui.Options{WorkingDir: dir}))

	out, err := d.Run(context.Background(), "/tools")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, dir) {
		t.Errorf("the report does not name the working directory:\n%s", out.Text)
	}
	for _, name := range []string{"git", "shell"} {
		if !strings.Contains(out.Text, name) {
			t.Errorf("the report does not list the %q tool:\n%s", name, out.Text)
		}
	}
}

// mustRead reads a file or fails the test, for the tests above that re-read the
// configuration file a handler just wrote.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
