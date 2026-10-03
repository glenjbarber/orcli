package saved

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCurrentNameIsTheDirectorysBaseName covers the naming, since a file a reader
// cannot predict the name of is a file they cannot restore by typing it.
//
// The base name rather than the whole path, for the reason autosaveName gives: a
// reader looking in the sessions directory finds the same file for the same tree, and
// a filename cannot hold a separator.
func TestCurrentNameIsTheDirectorysBaseName(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Base(dir) + DefaultSessionSuffix

	got, err := CurrentName(dir)
	if err != nil {
		t.Fatalf("CurrentName: %v", err)
	}
	if got != want {
		t.Errorf("CurrentName(%s) = %q, want %q", dir, got, want)
	}
}

// TestCurrentNameKeepsANonASCIIDirectoryWhole covers the case the cleaner was written
// for: two directories differing only in a non-ASCII character would produce one
// filename, and a reader who saved a session from each would find one of them gone.
func TestCurrentNameKeepsANonASCIIDirectoryWhole(t *testing.T) {
	parent := t.TempDir()
	one := filepath.Join(parent, "café")
	two := filepath.Join(parent, "cafe")
	for _, d := range []string{one, two} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	first, err := CurrentName(one)
	if err != nil {
		t.Fatalf("CurrentName: %v", err)
	}
	second, err := CurrentName(two)
	if err != nil {
		t.Fatalf("CurrentName: %v", err)
	}

	if first == second {
		t.Errorf("two directories named %q and %q gave one name, %q", one, two, first)
	}
	if !strings.Contains(first, "%") {
		t.Errorf("the name %q did not escape the non-ASCII character", first)
	}
}

// TestCurrentNameRefusesTheFilesystemRoot covers the case with no name to save
// under. A file named after nothing is a file nothing refers to.
func TestCurrentNameRefusesTheFilesystemRoot(t *testing.T) {
	if got, err := CurrentName("/"); err == nil {
		t.Errorf("CurrentName(/) = %q, want a refusal", got)
	}
}

// TestSaveCurrentWritesOneFileForTheDirectory is the defaulting, which is the whole
// point: a reader who saves twice in one directory gets one file, not one per turn.
func TestSaveCurrentWritesOneFileForTheDirectory(t *testing.T) {
	s := real(t)
	dir := t.TempDir()

	first, err := s.SaveCurrent(dir, Session{
		Model: "some/model",
		Turns: []Turn{{Role: "user", Content: "the first question"}},
	})
	if err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}

	second, err := s.SaveCurrent(dir, Session{
		Model: "some/model",
		Turns: []Turn{{Role: "user", Content: "the second question"}},
	})
	if err != nil {
		t.Fatalf("SaveCurrent again: %v", err)
	}

	if first != second {
		t.Errorf("two saves wrote %q and %q, want one file", first, second)
	}

	names, err := s.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 1 {
		t.Errorf("the store holds %v, want one session", names)
	}
}

// TestCurrentReadsBackWhatSaveCurrentWrote covers the round trip, which is the other
// half of the defaulting: the name the save writes under is the name the restore
// reads, or the feature is two halves that do not meet.
func TestCurrentReadsBackWhatSaveCurrentWrote(t *testing.T) {
	s := real(t)
	dir := t.TempDir()

	want := Session{
		Model:            "some/model",
		PromptTokens:     120,
		CompletionTokens: 34,
		TotalTokens:      154,
		Turns: []Turn{
			{Role: "user", Content: "a question"},
			{Role: "assistant", Content: "an answer"},
		},
	}
	if _, err := s.SaveCurrent(dir, want); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}

	got, err := s.Current(dir)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}

	if got.Name != filepath.Base(dir)+DefaultSessionSuffix {
		t.Errorf("the restored session is named %q, want the working name", got.Name)
	}
	if got.Model != want.Model {
		t.Errorf("the model came back as %q, want %q", got.Model, want.Model)
	}
	if got.TotalTokens != want.TotalTokens {
		t.Errorf("the total came back as %d, want %d", got.TotalTokens, want.TotalTokens)
	}
	if len(got.Turns) != len(want.Turns) {
		t.Fatalf("the session has %d turns, want %d", len(got.Turns), len(want.Turns))
	}
	for i := range got.Turns {
		if got.Turns[i].Role != want.Turns[i].Role ||
			got.Turns[i].Content != want.Turns[i].Content {
			t.Errorf("turn %d came back as %q/%q", i,
				got.Turns[i].Role, got.Turns[i].Content)
		}
	}
}

// TestCurrentReportsAnAbsentSession covers the case the feature turns on: a reader
// who ran /restore in a directory they have not worked in.
//
// It is an absence rather than a fault, and it is distinct from ErrNoStore, because a
// reader who asked for a session by name and a reader who asked for this
// directory's session are being told different things.
func TestCurrentReportsAnAbsentSession(t *testing.T) {
	s := real(t)

	_, err := s.Current(t.TempDir())
	if err == nil {
		t.Fatal("Current found a session in a directory that has none")
	}
	if !errors.Is(err, ErrNoSessionForDirectory) {
		t.Errorf("the refusal is %v, want ErrNoSessionForDirectory", err)
	}
	if errors.Is(err, ErrNoStore) {
		t.Error("the absence reads as a missing named session, which is a different thing")
	}
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("the refusal is %q, want it to say there is nothing to restore", err)
	}
}

// TestCurrentResolvesASymlinkedDirectory covers the case where a reader is in a
// checkout reached by a symlink. The file has to be the one for the directory they
// are actually in, or a save and a restore from the same shell disagree.
func TestCurrentResolvesASymlinkedDirectory(t *testing.T) {
	s := real(t)

	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("a symlink could not be made: %v", err)
	}

	if _, err := s.SaveCurrent(real, Session{Model: "m"}); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}

	if _, err := s.Current(link); err != nil {
		t.Errorf("Current through a symlink: %v", err)
	}
}

// TestSaveCurrentDoesNotOverwriteAnAutosave covers the collision the suffix exists to
// avoid.
//
// The autosave of a directory is written under the directory's base name, so a
// working session saved under the bare name would land on the autosave and be
// overwritten by the next clean turn.
func TestSaveCurrentDoesNotOverwriteAnAutosave(t *testing.T) {
	s := real(t)
	dir := t.TempDir()

	autosave, err := autosaveName(dir)
	if err != nil {
		t.Fatalf("autosaveName: %v", err)
	}
	if _, err := s.Save(Session{Name: autosave, Model: "m"}); err != nil {
		t.Fatalf("Save the autosave: %v", err)
	}

	working, err := s.SaveCurrent(dir, Session{Model: "m"})
	if err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}

	if filepath.Base(autosave)+".db" == filepath.Base(working) {
		t.Errorf("the working session and the autosave share the file %q", working)
	}
	if _, err := s.Load(autosave); err != nil {
		t.Errorf("the autosave was lost: %v", err)
	}
}

// TestCurrentNamesOffersOnlyWorkingSessions covers the completion source.
//
// Completing to a name that is not there teaches the reader that completion does not
// mean the thing exists, so a store with no working session in it offers nothing.
func TestCurrentNamesOffersOnlyWorkingSessions(t *testing.T) {
	s := real(t)
	dir := t.TempDir()

	if got, err := s.CurrentNames(); err != nil {
		t.Fatalf("CurrentNames: %v", err)
	} else if len(got) != 0 {
		t.Errorf("an empty store offered %v", got)
	}

	name, err := s.SaveCurrent(dir, Session{Model: "m"})
	if err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	if _, err := s.Save(Session{Name: "a-session-the-reader-named", Model: "m"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.CurrentNames()
	if err != nil {
		t.Fatalf("CurrentNames: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("CurrentNames gave %v, want one name", got)
	}
	if got[0]+".db" != filepath.Base(name) {
		t.Errorf("CurrentNames offered %q, want the file written at %q", got[0], name)
	}
}

// TestSaveCurrentKeepsTheTurnsOfAToolCall covers the envelope, since a working
// session is the one a reader is most likely to restore and a missing tool turn in
// it reads as though the model never called anything.
func TestSaveCurrentKeepsTheTurnsOfAToolCall(t *testing.T) {
	s := real(t)
	dir := t.TempDir()

	want := []Turn{
		{Role: "user", Content: "read the file"},
		{
			Role: "assistant",
			ToolCalls: []ToolTurn{{
				ID: "call_1", Type: "function", Index: 0,
				Name: "read_file", Args: `{"path":"x"}`, Result: "contents",
			}},
		},
		{Role: "tool", Content: "contents", Name: "read_file", ToolCallID: "call_1"},
	}
	if _, err := s.SaveCurrent(dir, Session{Model: "m", Turns: want}); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}

	got, err := s.Current(dir)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(got.Turns) != len(want) {
		t.Fatalf("the session has %d turns, want %d", len(got.Turns), len(want))
	}
	if len(got.Turns[1].ToolCalls) != 1 {
		t.Fatalf("the tool turn came back with %d calls, want 1", len(got.Turns[1].ToolCalls))
	}
	if got := got.Turns[1].ToolCalls[0]; got.Name != "read_file" || got.Args != `{"path":"x"}` {
		t.Errorf("the call came back as %q/%q", got.Name, got.Args)
	}
	if got := got.Turns[2]; got.Name != "read_file" || got.ToolCallID != "call_1" {
		t.Errorf("the answer came back as %q/%q", got.Name, got.ToolCallID)
	}
}

// TestCurrentOnANilStoreIsAnAbsence rather than a panic, since a caller that never
// opened a store should be told there is nothing to restore rather than crashing on
// the way to finding out.
func TestCurrentOnANilStoreIsAnAbsence(t *testing.T) {
	var s *Store
	if _, err := s.Current(t.TempDir()); !errors.Is(err, ErrNoSessionForDirectory) {
		t.Errorf("a nil store returned %v, want ErrNoSessionForDirectory", err)
	}
}
