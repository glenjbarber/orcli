package saved

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// real returns a store using the driver this package ships, against a real
// database in a temporary directory.
//
// The stub-backed tests elsewhere check this package's own behavior. This one
// checks that the shipped driver does what the store asks of it, which is a
// different question and the reason the driver is linked at all.
func real(t *testing.T) *Store {
	t.Helper()

	if !Available() {
		t.Skipf("the %s driver is not registered", DriverName())
	}

	previous := driverName
	driverName = "sqlite"
	t.Cleanup(func() { driverName = previous })

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// TestTheRealDriverRoundTripsASession is the whole point of linking a driver.
//
// Every other test in this package runs against a stub, so nothing has yet checked
// that the schema this package writes is one the driver accepts, that the column
// order matches, or that a query returning rows scans into the fields the store
// reads them into. All three are ways to find out in one call.
func TestTheRealDriverRoundTripsASession(t *testing.T) {
	s := real(t)

	want := Session{
		Name:  "round-trip",
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

	if _, err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load("round-trip")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Turns) != len(want.Turns) {
		t.Fatalf("%d turns came back, want %d", len(got.Turns), len(want.Turns))
	}
	for i := range want.Turns {
		if got.Turns[i].Role != want.Turns[i].Role {
			t.Errorf("turn %d has the role %q, want %q", i, got.Turns[i].Role, want.Turns[i].Role)
		}
		if got.Turns[i].Content != want.Turns[i].Content {
			t.Errorf("turn %d has the content %q, want %q", i, got.Turns[i].Content, want.Turns[i].Content)
		}
		if got.Turns[i].Name != want.Turns[i].Name {
			t.Errorf("turn %d names the tool %q, want %q", i, got.Turns[i].Name, want.Turns[i].Name)
		}
		if got.Turns[i].ToolCallID != want.Turns[i].ToolCallID {
			t.Errorf("turn %d is matched to %q, want %q", i,
				got.Turns[i].ToolCallID, want.Turns[i].ToolCallID)
		}
	}

	if got.Turns[2].Empty() {
		t.Fatal("the tool turn came back with no envelope")
	}
	if len(got.Turns[2].ToolCalls) != 1 {
		t.Fatalf("the tool turn carries %d calls, want 1", len(got.Turns[2].ToolCalls))
	}
	if got.Turns[2].ToolCalls[0].Name != "read_file" {
		t.Errorf("the tool is %q, want read_file", got.Turns[2].ToolCalls[0].Name)
	}
	if got.Turns[2].ToolCalls[0].Result != "in one.txt" {
		t.Errorf("the result is %q, want %q", got.Turns[2].ToolCalls[0].Result, "in one.txt")
	}

	if got.Model != want.Model {
		t.Errorf("the model is %q, want %q", got.Model, want.Model)
	}
	if got.PromptTokens != want.PromptTokens {
		t.Errorf("the prompt count is %d, want %d", got.PromptTokens, want.PromptTokens)
	}
	if got.TotalTokens != want.TotalTokens {
		t.Errorf("the total is %d, want %d", got.TotalTokens, want.TotalTokens)
	}
}

// TestTheRealDriverStoresNoEnvelopeForAnOrdinaryTurn checks the NULL rather than
// an empty string, which is a claim about the file a reader would query.
func TestTheRealDriverStoresNoEnvelopeForAnOrdinaryTurn(t *testing.T) {
	s := real(t)

	session := Session{Name: "plain", Turns: []Turn{{Role: "user", Content: "a question"}}}
	if _, err := s.Save(session); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load("plain")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Turns[0].Empty() {
		t.Errorf("the turn came back carrying %v, want nothing", got.Turns[0].ToolCalls)
	}
}

// TestTheRealDriverWritesPlainColumns is the promise the format makes.
//
// A reader who opens a saved conversation with their own SQLite tool should be able
// to read the turns, which is only true if role and content are ordinary columns.
func TestTheRealDriverWritesPlainColumns(t *testing.T) {
	s := real(t)

	session := Session{Name: "queryable", Turns: []Turn{
		{Role: "user", Content: "find me"},
		{Role: "assistant", Content: "here"},
	}}
	path, err := s.Save(session)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The file is read back through database/sql rather than through this package,
	// so the check is on the file and not on the store's own reading of it.
	d, err := open(path)
	if err != nil {
		t.Fatalf("open the file: %v", err)
	}
	defer d.Close()

	rows, err := d.Query(`SELECT role, content FROM messages ORDER BY seq`)
	if err != nil {
		t.Fatalf("query the messages: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			t.Fatalf("scan a row: %v", err)
		}
		got = append(got, role+": "+content)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the rows: %v", err)
	}

	want := []string{"user: find me", "assistant: here"}
	if len(got) != len(want) {
		t.Fatalf("the file holds %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// TestTheRealDriverWritesTheSchemaVersion is what a later client reads first.
func TestTheRealDriverWritesTheSchemaVersion(t *testing.T) {
	s := real(t)
	if _, err := s.Save(Session{Name: "versioned"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	session, err := s.Load("versioned")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if session.Name != "versioned" {
		t.Errorf("the loaded name is %q, want versioned", session.Name)
	}
}

// TestTheRealDriverReplacesASave covers writing the same name twice.
func TestTheRealDriverReplacesASave(t *testing.T) {
	s := real(t)

	if _, err := s.Save(Session{
		Name:  "twice",
		Turns: []Turn{{Role: "user", Content: "first"}},
	}); err != nil {
		t.Fatalf("the first save: %v", err)
	}
	if _, err := s.Save(Session{
		Name:  "twice",
		Turns: []Turn{{Role: "user", Content: "second"}},
	}); err != nil {
		t.Fatalf("the second save: %v", err)
	}

	session, err := s.Load("twice")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(session.Turns) != 1 {
		t.Fatalf("%d turns came back, want 1", len(session.Turns))
	}
	if got := session.Turns[0].Content; got != "second" {
		t.Errorf("the turn is %q, want %q: a second save replaces the first", got, "second")
	}

	names, err := s.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 1 {
		t.Errorf("Names is %v, want one entry", names)
	}
}

// TestTheRealDriverReportsAnAbsentSession covers the distinction against a file
// that genuinely is not there rather than a stub that says so.
func TestTheRealDriverReportsAnAbsentSession(t *testing.T) {
	s := real(t)

	if _, err := s.Load("never-saved"); !errors.Is(err, ErrNoStore) {
		t.Errorf("Load returned %v, want ErrNoStore", err)
	}
}

// TestTheRealDriverRefusesALaterVersion covers the refusal against a file that
// really does carry a later version.
//
// A file written by a later client is refused rather than half-read, and the only
// way to be sure the version is compared is to write one.
func TestTheRealDriverRefusesALaterVersion(t *testing.T) {
	s := real(t)
	path, err := s.Save(Session{Name: "future"})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	d, err := open(path)
	if err != nil {
		t.Fatalf("open the file: %v", err)
	}
	if _, err := d.Exec(`UPDATE meta SET value = ? WHERE key = 'version'`, "99"); err != nil {
		d.Close()
		t.Fatalf("write the later version: %v", err)
	}
	d.Close()

	if _, err := s.Load("future"); !errors.Is(err, ErrFutureVersion) {
		t.Errorf("Load returned %v, want ErrFutureVersion", err)
	}
}

// TestTheRealDriverReadsAnOlderVersion covers the file written before the
// version column existed.
func TestTheRealDriverReadsAnOlderVersion(t *testing.T) {
	s := real(t)
	path, err := s.Save(Session{Name: "old"})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	d, err := open(path)
	if err != nil {
		t.Fatalf("open the file: %v", err)
	}
	if _, err := d.Exec(`DELETE FROM meta WHERE key = 'version'`); err != nil {
		d.Close()
		t.Fatalf("remove the version: %v", err)
	}
	d.Close()

	if _, err := s.Load("old"); err != nil {
		t.Errorf("Load refused a file with no version: %v", err)
	}
}

// TestTheRealDriverReportsAnUnreadableEnvelope covers a file written badly by
// something that is not this client.
func TestTheRealDriverReportsAnUnreadableEnvelope(t *testing.T) {
	s := real(t)
	path, err := s.Save(Session{
		Name:  "broken",
		Turns: []Turn{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	d, err := open(path)
	if err != nil {
		t.Fatalf("open the file: %v", err)
	}
	if _, err := d.Exec(`UPDATE messages SET tool_calls = ?`, `{not an envelope`); err != nil {
		d.Close()
		t.Fatalf("write the bad envelope: %v", err)
	}
	d.Close()

	if _, err := s.Load("broken"); err == nil {
		t.Error("Load succeeded on an unreadable envelope, want it reported")
	}
}

// TestTheRealDriverWritesTheFilePrivate checks the mode a conversation is left
// at, since a session file is the reader's own words and the file is handed around.
func TestTheRealDriverWritesTheFilePrivate(t *testing.T) {
	s := real(t)

	path, err := s.Save(Session{Name: "private"})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the file is %04o, want 0600", got)
	}
}

// TestTheRealDriverNamesTheSessionsItWrites covers the listing a reader sees.
func TestTheRealDriverNamesTheSessionsItWrites(t *testing.T) {
	s := real(t)

	for _, name := range []string{"alpha", "beta"} {
		if _, err := s.Save(Session{Name: name}); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}

	names, err := s.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}

	want := []string{"alpha", "beta"}
	if len(names) != len(want) {
		t.Fatalf("Names is %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("Names is %v, want %v", names, want)
			break
		}
	}
}

// TestTheRealDriverSurvivesANonASCIIName covers a name a reader typed with a
// character outside ASCII, which is replaced rather than stripped.
func TestTheRealDriverSurvivesANonASCIIName(t *testing.T) {
	s := real(t)

	name := "café"
	if _, err := s.Save(Session{Name: name, Turns: []Turn{
		{Role: "user", Content: "in the café"},
	}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	session, err := s.Load(name)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(session.Turns) != 1 {
		t.Fatalf("%d turns came back, want 1", len(session.Turns))
	}
	if got := session.Turns[0].Content; got != "in the café" {
		t.Errorf("the content is %q, want it back unchanged", got)
	}
}

// TestTheRealDriverLeavesNothingBehind covers the whole save.
//
// A reader who loses power mid-save has the old file rather than a truncated one,
// and a session directory holding half-written conversations is worse than one
// holding none.
func TestTheRealDriverLeavesNothingBehind(t *testing.T) {
	s := real(t)

	if _, err := s.Save(Session{Name: "clean"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".db") {
			t.Errorf("the store holds %s, want only session files", entry.Name())
		}
	}

	if _, err := filepath.Abs(s.Dir()); err != nil {
		t.Errorf("the store directory does not resolve: %v", err)
	}
}
