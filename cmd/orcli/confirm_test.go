package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// overConfig points both confirmation seams at a temporary file, and returns a
// session and the path.
//
// Both are pointed rather than one, because a test that proves the model was written
// has to read it back, and a test that points only the writer would read the
// reader's own configuration file to do it.
func overConfig(t *testing.T, body string) (*tui.Session, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(body), config.FileMode); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	oldPath, oldRead, oldWrite := configPath, readModelPair, writeModel
	configPath = func() (string, error) { return path, nil }
	readModelPair = config.ReadModelPair
	writeModel = config.WriteModel

	t.Cleanup(func() {
		configPath, readModelPair, writeModel = oldPath, oldRead, oldWrite
	})

	return tui.New(tui.Options{Model: "some/model", APIKey: "k"}), path
}

// readModel returns what is at path.
func readModel(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestAConfirmedModelIsWritten covers the connection there is no command for. A turn
// that came back with text has proved the credential and the model together, and that
// is what writes the model.
func TestAConfirmedModelIsWritten(t *testing.T) {
	s, path := overConfig(t, "{\n  \"api_key\": \"k\"\n}\n")

	confirmModel(s)("some/model")

	if got := readModel(t, path); !strings.Contains(got, `"model": "some/model"`) {
		t.Errorf("the model was not written:\n%s", got)
	}
}

// TestTheWriteKeepsTheCredentialAndTheKeyOrder covers what WriteModel is for. The
// file holds a credential, so a writer that decoded it and re-encoded it would
// reorder the keys and a member this client does not name would be lost.
func TestTheWriteKeepsTheCredentialAndTheKeyOrder(t *testing.T) {
	s, path := overConfig(t, "{\n  \"api_key\": \"k\"\n}\n")

	confirmModel(s)("some/model")

	got := readModel(t, path)
	if !strings.Contains(got, `"api_key": "k"`) {
		t.Errorf("the credential did not survive the write:\n%s", got)
	}
	key, model := strings.Index(got, `"api_key"`), strings.Index(got, `"model"`)
	if key < 0 || model < 0 {
		t.Fatalf("a member went missing:\n%s", got)
	}
	if key > model {
		t.Errorf("the members were reordered:\n%s", got)
	}
}

// TestTheWriteKeepsASingleLineFileOnOneLine covers the property every writer in that
// package works to preserve, since the reader may well have written the file that way.
func TestTheWriteKeepsASingleLineFileOnOneLine(t *testing.T) {
	s, path := overConfig(t, `{"api_key":"k"}`)

	confirmModel(s)("some/model")

	got := readModel(t, path)
	if strings.Contains(got, "\n") {
		t.Errorf("a single-line file came back over several lines:\n%q", got)
	}
	if !strings.HasPrefix(got, `{"api_key":"k","model":`) {
		t.Errorf("the shape of what the reader wrote changed: %q", got)
	}
}

// TestAnEmptyModelIsNotWritten covers the case where a turn came back with no model
// named. There is nothing to record, and a write of an empty string would put a
// member in the file that means the reader chose nothing rather than that nothing
// happened.
func TestAnEmptyModelIsNotWritten(t *testing.T) {
	s, path := overConfig(t, "{\n  \"api_key\": \"k\"\n}\n")
	before := readModel(t, path)

	confirmModel(s)("")

	if got := readModel(t, path); got != before {
		t.Errorf("an empty model was written:\n%s", got)
	}
}

// TestTheModelIsWrittenOnce covers the once-per-session rule. The reader's first
// question is the probe, and every turn after that writing the same model again is a
// write the reader did not ask for.
func TestTheModelIsWrittenOnce(t *testing.T) {
	s, path := overConfig(t, "{\n  \"api_key\": \"k\"\n}\n")

	var writes int
	oldWrite := writeModel
	writeModel = func(p, model string) error {
		writes++
		return oldWrite(p, model)
	}
	t.Cleanup(func() { writeModel = oldWrite })

	confirm := confirmModel(s)
	confirm("some/model")
	confirm("some/model")
	confirm("some/model")

	if writes != 1 {
		t.Errorf("the model was written %d times, want once", writes)
	}
	if got := readModel(t, path); !strings.Contains(got, `"model": "some/model"`) {
		t.Errorf("the model was not written:\n%s", got)
	}
}

// TestAConfirmedModelAlreadyOnDiskIsNotWritten covers the common case this fix is
// for: a reader who already set up orcli and is simply using it again. The model
// that answers the first turn is already in the file, so there is nothing to write
// and nothing to tell the reader about.
func TestAConfirmedModelAlreadyOnDiskIsNotWritten(t *testing.T) {
	s, path := overConfig(t, "{\n  \"api_key\": \"k\",\n  \"model\": \"some/model\"\n}\n")
	before := readModel(t, path)

	var writes int
	oldWrite := writeModel
	writeModel = func(p, model string) error {
		writes++
		return oldWrite(p, model)
	}
	t.Cleanup(func() { writeModel = oldWrite })

	confirmModel(s)("some/model")

	if writes != 0 {
		t.Errorf("the model was written %d times, want zero", writes)
	}
	if got := readModel(t, path); got != before {
		t.Errorf("the file changed even though nothing did:\n%s", got)
	}
	for _, row := range s.Log().Rows() {
		if strings.Contains(row.Text, "is written to") {
			t.Errorf("a notice was shown for a write that did not happen: %q", row.Text)
		}
	}
}

// TestAConfirmedDifferentModelIsStillWritten covers the other half of the same
// check: a session confirming a model that differs from the one on disk still
// writes it and still tells the reader, exactly as before this check existed.
func TestAConfirmedDifferentModelIsStillWritten(t *testing.T) {
	s, path := overConfig(t, "{\n  \"api_key\": \"k\",\n  \"model\": \"old/model\"\n}\n")

	confirmModel(s)("new/model")

	got := readModel(t, path)
	if !strings.Contains(got, `"model": "new/model"`) {
		t.Errorf("the changed model was not written:\n%s", got)
	}

	rows := s.Log().Rows()
	if len(rows) == 0 {
		t.Fatalf("nothing was reported to the reader")
	}
	last := rows[len(rows)-1]
	if !strings.Contains(last.Text, "is written to") {
		t.Errorf("the reader was not told about the write: %q", last.Text)
	}
}

// TestTheOnceGuardStillAppliesWhenNothingWasWritten covers the interaction between
// the two checks: a session that already found the model unchanged on its first
// turn is not asked again on a later turn, even though it never wrote anything.
func TestTheOnceGuardStillAppliesWhenNothingWasWritten(t *testing.T) {
	s, _ := overConfig(t, "{\n  \"api_key\": \"k\",\n  \"model\": \"some/model\"\n}\n")

	var reads int
	oldRead := readModelPair
	readModelPair = func(p string) (string, string, error) {
		reads++
		return oldRead(p)
	}
	t.Cleanup(func() { readModelPair = oldRead })

	confirm := confirmModel(s)
	confirm("some/model")
	confirm("some/model")
	confirm("some/model")

	if reads != 1 {
		t.Errorf("the configuration was read %d times, want once", reads)
	}
}

// TestAFailedWriteIsReportedAndNotRetried covers the nuisance case. The turn was
// answered and the answer is what the reader asked for, so a model that could not be
// written is a row in the log rather than something that loses the reply.
func TestAFailedWriteIsReportedAndNotRetried(t *testing.T) {
	s, _ := overConfig(t, "{\n  \"api_key\": \"k\"\n}\n")

	var writes int
	writeModel = func(string, string) error {
		writes++
		return os.ErrPermission
	}
	t.Cleanup(func() { writeModel = config.WriteModel })

	confirm := confirmModel(s)
	confirm("some/model")
	confirm("some/model")

	if writes != 1 {
		t.Errorf("a failed write was attempted %d times, want once", writes)
	}

	rows := s.Log().Rows()
	if len(rows) < 2 {
		t.Fatalf("nothing was reported to the reader:\n%v", rows)
	}
	last := rows[len(rows)-1]
	if !strings.Contains(last.Text, "writing it failed") {
		t.Errorf("the reader was told %q, want the write failure", last.Text)
	}
}

// TestNothingIsWrittenWithoutAConfirmedTurn is the rule the reader named, and it is
// what the file looks like on the first screen: the credential is there, the model is
// not, and nothing has been asked.
func TestNothingIsWrittenWithoutAConfirmedTurn(t *testing.T) {
	_, path := overConfig(t, "{\n  \"api_key\": \"k\"\n}\n")

	got := readModel(t, path)
	if strings.Contains(got, `"model"`) {
		t.Errorf("a model was in the file before any turn was answered:\n%s", got)
	}
}
