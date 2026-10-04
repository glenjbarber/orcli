package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trustFile writes a configuration file carrying a credential and returns its
// path.
//
// The mode is set rather than left to the umask, because the writers under test
// refuse anything but 0600 and a test that could not set the mode would be a test
// that only passed on a machine with an unusual umask.
func trustFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	if err := os.Chmod(path, FileMode); err != nil {
		t.Fatalf("set the mode: %v", err)
	}
	return path
}

// readFile returns what is at path.
func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestAddTrustedRecordsTheDirectory covers the ordinary case. Trust is asked once
// per directory, so a directory that was asked about has to end up in the file or
// the reader is asked again on every run.
func TestAddTrustedRecordsTheDirectory(t *testing.T) {
	const dir = "/home/reader/project"
	path := trustFile(t, `{"api_key":"k"}`)

	if err := AddTrusted(path, []string{dir}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}

	if got := readFile(t, path); !strings.Contains(got, dir) {
		t.Errorf("the file does not carry the directory:\n%s", got)
	}
}

// TestAddTrustedKeepsTheCredentialIntact is the test that matters most here,
// since the file holds a credential. An edit that dropped or reordered the key
// would be a corrupt credential file discovered at the next request.
func TestAddTrustedKeepsTheCredentialIntact(t *testing.T) {
	path := trustFile(t, `{"api_key":"sk-or-v1-kept","model":"some/model"}`)

	if err := AddTrusted(path, []string{"/home/reader/project"}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}

	cfg, err := Parse([]byte(readFile(t, path)), path)
	if err != nil {
		t.Fatalf("the edited file does not parse: %v\n%s", err, readFile(t, path))
	}
	if cfg.APIKey != "sk-or-v1-kept" {
		t.Errorf("the credential is %q, want it unchanged", cfg.APIKey)
	}
	if cfg.Model != "some/model" {
		t.Errorf("the model is %q, want it unchanged", cfg.Model)
	}
}

// TestAddTrustedDoesNotDuplicate covers the case where a directory is already
// listed. A list carrying the same directory twice is a file a reader has to edit
// by hand before they believe it.
func TestAddTrustedDoesNotDuplicate(t *testing.T) {
	const dir = "/home/reader/project"
	path := trustFile(t, `{"api_key":"k","`+trustedKey+`":["`+dir+`"]}`)

	if err := AddTrusted(path, []string{dir}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}

	if got := strings.Count(readFile(t, path), dir); got != 1 {
		t.Errorf("the directory appears %d times, want 1:\n%s", got, readFile(t, path))
	}
}

// TestAddTrustedAddsToAnExistingList covers the case where the list already holds
// something. A file with a trust list in it is a file a reader has already used,
// and the edit must not drop what is there.
func TestAddTrustedAddsToAnExistingList(t *testing.T) {
	path := trustFile(t, `{"api_key":"k","`+trustedKey+`":["/home/reader/first"]}`)

	if err := AddTrusted(path, []string{"/home/reader/second"}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}

	got := readFile(t, path)
	for _, want := range []string{"/home/reader/first", "/home/reader/second"} {
		if !strings.Contains(got, want) {
			t.Errorf("the file does not carry %q:\n%s", want, got)
		}
	}
}

// TestAddTrustedChangesNothingWhenThereIsNothingToAdd covers the quiet path. A
// writer that rewrites a file to change nothing loses the reader's hand for no
// reason at all.
func TestAddTrustedChangesNothingWhenThereIsNothingToAdd(t *testing.T) {
	path := trustFile(t, `{"api_key":"k"}`)

	if err := AddTrusted(path, []string{"/home/reader/first"}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}
	first := readFile(t, path)

	// The same directory again, which is already there.
	if err := AddTrusted(path, []string{"/home/reader/first"}); err != nil {
		t.Fatalf("AddTrusted again: %v", err)
	}
	if got := readFile(t, path); got != first {
		t.Errorf("a second write changed the file:\nfirst:\n%s\nsecond:\n%s", first, got)
	}
}

// TestAddTrustedRefusesABadMode covers the check the other writers make. A trust
// record is not a reason to bypass the mode on a file holding a credential.
func TestAddTrustedRefusesABadMode(t *testing.T) {
	path := trustFile(t, `{"api_key":"k"}`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	err := AddTrusted(path, []string{"/home/reader/project"})
	if err == nil {
		t.Fatal("AddTrusted wrote to a file at mode 0644, want a refusal")
	}
	if !errors.Is(err, ErrBadMode) {
		t.Errorf("the refusal is %v, want it to carry ErrBadMode", err)
	}
}

// TestAddTrustedRefusesAFileThatIsNotAnObject covers the shape check. A file that
// is valid JSON but not an object cannot carry a member, and writing one anyway
// would corrupt a file the reader wrote.
func TestAddTrustedRefusesAFileThatIsNotAnObject(t *testing.T) {
	path := trustFile(t, `["not","an","object"]`)

	if err := AddTrusted(path, []string{"/home/reader/project"}); err == nil {
		t.Fatal("AddTrusted wrote to a file that is not an object, want a refusal")
	}
	if got := readFile(t, path); !strings.Contains(got, "not") {
		t.Errorf("the file was changed by a refused write:\n%s", got)
	}
}

// TestAddTrustedKeepsTheFileShape covers the property that makes a hand-written
// configuration survive. Key order, spacing and the final newline are the
// reader's, and a writer that tidies them has rewritten the file rather than
// edited it.
func TestAddTrustedKeepsTheFileShape(t *testing.T) {
	// Key order as written, no spaces after the colons, and no final newline.
	path := trustFile(t, `{"model":"some/model","api_key":"k"}`)

	if err := AddTrusted(path, []string{"/home/reader/project"}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}

	got := readFile(t, path)
	if !strings.HasPrefix(got, `{"model":"some/model","api_key":"k",`) {
		t.Errorf("the edit changed the shape of what the reader wrote:\n%s", got)
	}
}

// TestAddTrustedQuotesAPathThatNeedsIt covers the escaping, since a directory is a
// path and a path can carry a backslash or a quote on any of the systems this
// builds for.
func TestAddTrustedQuotesAPathThatNeedsIt(t *testing.T) {
	path := trustFile(t, `{"api_key":"k"}`)

	if err := AddTrusted(path, []string{`C:\Users\reader\project`}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}

	cfg, err := Parse([]byte(readFile(t, path)), path)
	if err != nil {
		t.Fatalf("the edited file does not parse: %v\n%s", err, readFile(t, path))
	}
	if len(cfg.Trusted) != 1 || cfg.Trusted[0] != `C:\Users\reader\project` {
		t.Errorf("the directory came back as %q", cfg.Trusted)
	}
}

// TestAddTrustedOnAMissingMember adds the member rather than failing, since a
// reader who has never approved a directory has no list to extend.
func TestAddTrustedOnAMissingMember(t *testing.T) {
	path := trustFile(t, `{"api_key":"k"}`)

	if err := AddTrusted(path, []string{"/home/reader/project"}); err != nil {
		t.Fatalf("AddTrusted: %v", err)
	}

	cfg, err := Parse([]byte(readFile(t, path)), path)
	if err != nil {
		t.Fatalf("the edited file does not parse: %v", err)
	}
	if len(cfg.Trusted) != 1 {
		t.Fatalf("the file carries %d directories, want 1", len(cfg.Trusted))
	}
}

// TestAddTrustedOnATrustListThatIsNotAList refuses rather than replacing it.
//
// A file whose trust list is a string rather than an array is a file somebody
// edited by hand, and overwriting their entry with a list of directories this
// client chose is not an edit.
func TestAddTrustedOnATrustListThatIsNotAList(t *testing.T) {
	path := trustFile(t, `{"api_key":"k","`+trustedKey+`":"/home/reader"}`)

	if err := AddTrusted(path, []string{"/home/reader/project"}); err == nil {
		t.Fatal("AddTrusted replaced a trust entry that was not a list, want a refusal")
	}
	if got := readFile(t, path); !strings.Contains(got, `"/home/reader"`) {
		t.Errorf("the file was changed by a refused write:\n%s", got)
	}
}

// TestTheTrustMemberIsSpelledForThisProgram holds the rename. The record of what
// this client was allowed to do in a directory is a fact about this program, and
// a member spelled for the endpoint would say the record belongs to the endpoint
// rather than to the thing that obeyed it.
func TestTheTrustMemberIsSpelledForThisProgram(t *testing.T) {
	if trustedKey != "ORCLI_TRUSTED" {
		t.Errorf("the trust member is %q, want ORCLI_TRUSTED", trustedKey)
	}
	if readableKey != "ORCLI_READABLE" {
		t.Errorf("the readable member is %q, want ORCLI_READABLE", readableKey)
	}
}

// TestAnOldMemberIsIgnoredRatherThanMisread is what the rename costs and why it is
// safe. A file carrying the old spelling reads as though the reader approved
// nothing, so every directory is asked about again, which is a question rather
// than a refusal: the reader is told what is happening and can approve it.
//
// It is not read as a fault, because a fault at startup over a member this
// version no longer uses would lock a reader out of their own configuration.
func TestAnOldMemberIsIgnoredRatherThanMisread(t *testing.T) {
	dir := t.TempDir()
	path := trustFile(t, `{"api_key":"k","OPENROUTER_TRUSTED":["`+dir+`"]}`)

	cfg, err := Parse([]byte(readFile(t, path)), path)
	if err != nil {
		t.Fatalf("a file carrying the old member was refused: %v", err)
	}
	if len(cfg.Trusted) != 0 {
		t.Errorf("the old member was read as %v, want it ignored", cfg.Trusted)
	}
}
