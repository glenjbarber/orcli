package saved

import (
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// store returns a store pointed at a temporary directory, using the stub driver, with
// the stub reporting every session as present.
//
// The stub cannot create files, so a test that loads has to say the file is there. Doing
// it here rather than in each test keeps that detail out of the tests that are about
// something else.
func store(t *testing.T) *Store {
	t.Helper()

	reset()
	t.Cleanup(reset)

	previous := driverName
	driverName = "stub"
	t.Cleanup(func() { driverName = previous })

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// present tells the stub that a named session exists, and returns the path it would
// be at.
func present(t *testing.T, s *Store, name string) string {
	t.Helper()

	path, err := s.Path(name)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// sample is a session with one of everything, so a round trip can be checked against
// it.
func sample() Session {
	return Session{
		Name:  "work",
		Model: "stealth/space-bunny-alpha",
		Turns: []Turn{
			{Role: "system", Content: "instructions"},
			{Role: "user", Content: "read one.txt"},
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolTurn{{
					ID: "call_a", Type: "function", Index: 0,
					Name: "read_file", Args: `{"path":"one.txt"}`,
					Result: "in one.txt",
				}},
			},
			{Role: "tool", Name: "read_file", ToolCallID: "call_a", Content: "in one.txt"},
			{Role: "assistant", Content: "it says: in one.txt"},
		},
		PromptTokens:     120,
		CompletionTokens: 34,
		TotalTokens:      154,
	}
}

// recordedMeta returns the value written under a metadata key.
//
// The key is looked for in the arguments rather than in the query text, since a
// parameterised statement carries it as a placeholder and the text says nothing about
// which row this is.
func recordedMeta(key string) []string {
	var out []string
	for _, statement := range recorded("INSERT INTO meta") {
		values := statement.values()
		if len(values) >= 2 && values[0] == key {
			out = append(out, values[1])
		}
	}
	return out
}

// TestSaveWritesTheSchemaVersion covers what a later client reads first.
func TestSaveWritesTheSchemaVersion(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	versions := recordedMeta("version")
	if len(versions) != 1 {
		t.Fatalf("the version is recorded %d times, want once", len(versions))
	}
	if versions[0] != strconv.Itoa(schemaVersion) {
		t.Errorf("the version is recorded as %q, want %d", versions[0], schemaVersion)
	}
}

// TestSaveRecordsTheMetadataInAFixedOrder covers the file being reproducible.
//
// The metadata is written in a fixed order rather than by ranging a map, so a file
// written twice has the same rows in the same order. A file differing only in row order
// is a file two sessions disagree about.
func TestSaveRecordsTheMetadataInAFixedOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var keys []string
	for _, statement := range recorded("INSERT INTO meta") {
		keys = append(keys, statement.values()[0])
	}

	want := []string{"version", "model", "prompt_tokens", "completion_tokens", "total_tokens"}
	if len(keys) != len(want) {
		t.Fatalf("the metadata is %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("the metadata is %v, want %v", keys, want)
			break
		}
	}
}

// TestSaveWritesPlainColumns covers the reason the format is a database.
//
// A reader who opens a saved conversation with their own SQLite tool should be able to
// read the turns without this client being involved, which is why role and content are
// ordinary columns rather than something inside an envelope.
func TestSaveWritesPlainColumns(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	statements := recorded("INSERT INTO messages")
	if len(statements) == 0 {
		t.Fatal("no turn was written")
	}
	for _, column := range []string{"seq", "role", "content", "name", "tool_call_id", "tool_calls"} {
		if !strings.Contains(statements[0].query, column) {
			t.Errorf("the messages table has no %s column: %s", column, statements[0].query)
		}
	}
}

// TestSaveWritesATurnInOrder covers what a loaded conversation reads back as.
func TestSaveWritesATurnInOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	statements := recorded("INSERT INTO messages")
	if len(statements) != 5 {
		t.Fatalf("%d turns were written, want 5", len(statements))
	}
	for i, statement := range statements {
		if got := statement.values()[0]; got != strconv.Itoa(i) {
			t.Errorf("turn %d was written with sequence %q, want %d", i, got, i)
		}
	}
}

// TestSaveWritesTheRolesInOrder covers that the turns come back in the order they
// were said rather than in the order a query happened to return them.
func TestSaveWritesTheRolesInOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	want := []string{"system", "user", "assistant", "tool", "assistant"}
	statements := recorded("INSERT INTO messages")
	for i, role := range want {
		if got := statements[i].values()[1]; got != role {
			t.Errorf("turn %d has the role %q, want %q", i, got, role)
		}
	}
}

// TestSaveStoresNoEnvelopeForAnOrdinaryTurn covers the space a later field needs.
//
// An ordinary turn stores no envelope at all rather than an empty one, so a column added
// later has somewhere to go without migrating the files already written.
func TestSaveStoresNoEnvelopeForAnOrdinaryTurn(t *testing.T) {
	s := store(t)

	session := Session{Name: "plain", Turns: []Turn{{Role: "user", Content: "a question"}}}
	if _, err := s.Save(session); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !session.Turns[0].Empty() {
		t.Error("an ordinary turn reports itself as carrying an envelope")
	}

	values := recorded("INSERT INTO messages")[0].values()
	if envelope := values[len(values)-1]; envelope != "<nil>" {
		t.Errorf("the envelope column is %q, want SQL NULL rather than an empty string",
			envelope)
	}
}

// TestSaveWritesAnEnvelopeForAToolTurn covers the tool case.
func TestSaveWritesAnEnvelopeForAToolTurn(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	statements := recorded("INSERT INTO messages")
	if len(statements) < 3 {
		t.Fatalf("%d turns were written, want at least 3", len(statements))
	}
	// The third turn is the assistant turn carrying the call.
	values := statements[2].values()
	if envelope := values[len(values)-1]; !strings.Contains(envelope, "read_file") {
		t.Errorf("the envelope is %q, want it to name the tool", envelope)
	}
}

// TestSaveMatchesAnAnswerToItsCall covers the columns that pair them.
func TestSaveMatchesAnAnswerToItsCall(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	values := recorded("INSERT INTO messages")[3].values()
	if got := values[3]; got != "read_file" {
		t.Errorf("the answer names the tool %q, want read_file", got)
	}
	if got := values[4]; got != "call_a" {
		t.Errorf("the answer is matched to %q, want call_a", got)
	}
}

// TestSaveRecordsTheCounts covers what a loaded session reports.
//
// The counts are per conversation and are restored by a load, unlike the session ledger,
// which is not.
func TestSaveRecordsTheCounts(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for key, want := range map[string]string{
		"model":             "stealth/space-bunny-alpha",
		"prompt_tokens":     "120",
		"completion_tokens": "34",
		"total_tokens":      "154",
	} {
		got := recordedMeta(key)
		if len(got) != 1 {
			t.Errorf("%s is recorded %d times, want once", key, len(got))
			continue
		}
		if got[0] != want {
			t.Errorf("%s is recorded as %q, want %q", key, got[0], want)
		}
	}
}

// TestSaveRefusesANameThatWouldEscape covers the naming rule.
//
// A reader who typed ../outside meant to leave this directory, and a store that turned
// that into a filename has silently answered a different question than the one asked.
func TestSaveRefusesANameThatWouldEscape(t *testing.T) {
	s := store(t)

	for _, name := range []string{"", "   ", "../outside", "a/b", `a\b`, ".", ".."} {
		t.Run(name, func(t *testing.T) {
			session := sample()
			session.Name = name
			if _, err := s.Save(session); err == nil {
				t.Errorf("the name %q was accepted, want it refused", name)
			}
		})
	}
}

// TestLoadRefusesALaterVersion covers the decision to refuse rather than half-read.
//
// A conversation missing the turns a tool call made reads as though the model never
// called anything, which is a wrong answer rather than a missing one.
func TestLoadRefusesALaterVersion(t *testing.T) {
	s := store(t)
	present(t, s, "later")
	stub.meta = map[string]string{"version": "9", "model": "m"}

	if _, err := s.Load("later"); !errors.Is(err, ErrFutureVersion) {
		t.Errorf("Load returned %v, want ErrFutureVersion", err)
	}
}

// TestLoadNamesBothVersions covers what the refusal says.
//
// A reader told "written by a later version" learns less than one told which version
// wrote it and which is reading it.
func TestLoadNamesBothVersions(t *testing.T) {
	s := store(t)
	present(t, s, "later")
	stub.meta = map[string]string{"version": "9"}

	_, err := s.Load("later")
	if err == nil {
		t.Fatal("Load succeeded, want a refusal")
	}
	for _, want := range []string{"9", "2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is %q, want it to name %s", err, want)
		}
	}
}

// TestLoadReadsAnOlderVersion covers the file written before the column was added.
func TestLoadReadsAnOlderVersion(t *testing.T) {
	s := store(t)
	present(t, s, "older")
	stub.meta = map[string]string{"model": "m"}

	if _, err := s.Load("older"); err != nil {
		t.Errorf("Load refused a version 1 file: %v", err)
	}
}

// TestLoadReadsTheTurnsBack covers the round trip.
func TestLoadReadsTheTurnsBack(t *testing.T) {
	s := store(t)
	present(t, s, "work")
	stub.rows = [][]driver.Value{
		{"system", "instructions", nil, nil, nil},
		{"user", "read one.txt", nil, nil, nil},
		{"assistant", "", nil, nil, `[{"id":"call_a","type":"function","index":0,` +
			`"name":"read_file","args":"{\"path\":\"one.txt\"}","result":"in one.txt"}]`},
		{"tool", "in one.txt", "read_file", "call_a", nil},
		{"assistant", "it says: in one.txt", nil, nil, nil},
	}
	stub.meta = map[string]string{
		"version": "2", "model": "stealth/space-bunny-alpha",
		"prompt_tokens": "120", "completion_tokens": "34", "total_tokens": "154",
	}

	session, err := s.Load("work")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(session.Turns) != 5 {
		t.Fatalf("%d turns came back, want 5", len(session.Turns))
	}
	if got, want := session.Turns[0].Content, "instructions"; got != want {
		t.Errorf("the first turn is %q, want %q", got, want)
	}
	if session.Turns[2].Empty() {
		t.Fatal("the tool turn came back with no envelope")
	}
	if got, want := session.Turns[2].ToolCalls[0].Name, "read_file"; got != want {
		t.Errorf("the tool is %q, want %q", got, want)
	}
	if got, want := session.Turns[3].ToolCallID, "call_a"; got != want {
		t.Errorf("the answer is matched to %q, want %q", got, want)
	}
	if got, want := session.TotalTokens, 154; got != want {
		t.Errorf("the total is %d, want %d", got, want)
	}
	if got, want := session.Model, "stealth/space-bunny-alpha"; got != want {
		t.Errorf("the model is %q, want %q", got, want)
	}
}

// TestLoadTreatsAZeroAsAZero covers a count that really was zero.
func TestLoadTreatsAZeroAsAZero(t *testing.T) {
	s := store(t)
	present(t, s, "fresh")
	stub.meta = map[string]string{"version": "2", "total_tokens": "0"}

	session, err := s.Load("fresh")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if session.TotalTokens != 0 {
		t.Errorf("the total is %d, want 0", session.TotalTokens)
	}
}

// TestLoadReportsAnAbsentSessionAsAState covers the distinction.
//
// A session named by a reader who has not saved one yet is a session they are about to
// create, which is not a fault.
func TestLoadReportsAnAbsentSessionAsAState(t *testing.T) {
	s := store(t)

	if _, err := s.Load("nothing-here"); !errors.Is(err, ErrNoStore) {
		t.Errorf("Load returned %v, want ErrNoStore", err)
	}
}

// TestLoadReportsAnUnreadableFileAsAFault covers the other half.
func TestLoadReportsAnUnreadableFileAsAFault(t *testing.T) {
	s := store(t)
	present(t, s, "broken")
	stub.failOpen = true

	_, err := s.Load("broken")
	switch {
	case err == nil:
		t.Error("Load succeeded on a file that cannot be opened, want a fault")
	case errors.Is(err, ErrNoStore):
		t.Errorf("a file that cannot be read is reported as %v, want a fault", err)
	}
}

// TestLoadReportsAnUnreadableEnvelope covers a file written badly by something else.
func TestLoadReportsAnUnreadableEnvelope(t *testing.T) {
	s := store(t)
	present(t, s, "broken-envelope")
	stub.meta = map[string]string{"version": "2"}
	stub.rows = [][]driver.Value{
		{"assistant", "", nil, nil, `{not an envelope`},
	}

	if _, err := s.Load("broken-envelope"); err == nil {
		t.Error("Load succeeded on an unreadable envelope, want it reported")
	}
}

// TestTheSaveIsWhole covers the rename.
//
// A reader who loses power mid-save has the old file rather than a truncated one, and a
// conversation is the one thing here that cannot be recovered from anywhere else.
func TestTheSaveIsWhole(t *testing.T) {
	s := store(t)
	if _, err := s.Save(sample()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".save-") {
			t.Errorf("a temporary file survived the save: %s", entry.Name())
		}
	}
}

// TestANameCannotCarryANonASCIICharacter covers the replacement rule.
//
// Each is replaced rather than stripped, since stripping would give two directories
// differing only in a non-ASCII character one file.
func TestANameCannotCarryANonASCIICharacter(t *testing.T) {
	s := store(t)

	one, err := s.Path("café")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	two, err := s.Path("cafe")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if one == two {
		t.Error("a name with a non-ASCII character and one without share a file")
	}
	if strings.ContainsAny(filepath.Base(one), `/\`) {
		t.Errorf("the file is %q, want no separator in its name", one)
	}
}

// TestALiteralEscapeIsDoubledFirst covers the ordering.
//
// A name carrying one must not be turned into the name that would have been written
// without it, so the doubling happens before anything else is replaced.
func TestALiteralEscapeIsDoubledFirst(t *testing.T) {
	clean, err := sessionName("a%b")
	if err != nil {
		t.Fatalf("sessionName: %v", err)
	}
	if clean != "a%%b" {
		t.Errorf("the name became %q, want %q", clean, "a%%b")
	}
}

// TestAutosaveIsNamedForTheDirectory covers the autosave name.
func TestAutosaveIsNamedForTheDirectory(t *testing.T) {
	name, err := autosaveName(t.TempDir())
	if err != nil {
		t.Fatalf("autosaveName: %v", err)
	}
	if name == "" || strings.ContainsAny(name, `/\`) {
		t.Errorf("the autosave name is %q, want a bare directory name", name)
	}
}

// TestOpenCreatesTheDirectory covers a first run.
func TestOpenCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Dir() != dir {
		t.Errorf("the store is at %q, want %q", s.Dir(), dir)
	}
}

// TestOpenRefusesAFileAtThePath covers the path a reader put there.
func TestOpenRefusesAFileAtThePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Open(dir); err == nil {
		t.Error("Open accepted a file at the sessions path, want it reported")
	}
}

// TestNamesSkipsWhatIsNotASession covers the directory being more than sessions.
func TestNamesSkipsWhatIsNotASession(t *testing.T) {
	s := store(t)

	if err := os.WriteFile(filepath.Join(s.Dir(), "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(s.Dir(), "sub.db"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	names, err := s.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("Names is %v, want nothing: neither of those is a session", names)
	}
}
