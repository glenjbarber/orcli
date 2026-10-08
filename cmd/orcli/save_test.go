package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/saved"
	"github.com/glenjbarber/orcli/internal/tui"
)

// saveDispatcher is a dispatcher whose session is set, pointed at a fake HOME so
// /save and /load never touch a real reader's ~/.orcli.
func saveDispatcher(t *testing.T) *dispatcher {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{Model: "some/model"}))
	return d
}

// TestSaveRefusesAnEmptyName covers the one way /save is typed with nothing to
// name the file by.
func TestSaveRefusesAnEmptyName(t *testing.T) {
	d := saveDispatcher(t)

	if _, err := d.save("   "); err != ErrNoSaveName {
		t.Fatalf("save(\"   \"): got %v, want ErrNoSaveName", err)
	}
}

// TestLoadRefusesAnEmptyName covers /load's side of the same rule.
func TestLoadRefusesAnEmptyName(t *testing.T) {
	d := saveDispatcher(t)

	if _, err := d.load("   "); err != ErrNoLoadName {
		t.Fatalf("load(\"   \"): got %v, want ErrNoLoadName", err)
	}
}

// TestLoadingANameNeverSavedIsReported covers a reader typing a name they have
// not saved yet: reported as a state, not a crash.
func TestLoadingANameNeverSavedIsReported(t *testing.T) {
	d := saveDispatcher(t)

	_, err := d.load("nothing-here")
	if err == nil {
		t.Fatal("loading a name that was never saved was accepted")
	}
	if !errors.Is(err, saved.ErrNoStore) {
		t.Errorf("the error is %v, want it to wrap saved.ErrNoStore", err)
	}
}

// TestSaveThenLoadRoundTripsTheConversation covers the seam itself: a
// conversation written by /save comes back through /load the way it went in.
func TestSaveThenLoadRoundTripsTheConversation(t *testing.T) {
	d := saveDispatcher(t)

	d.session.Notice("first notice", 0, tui.RoleNotice)
	d.session.Deliver("a reply", 0)

	out, err := d.save("my-conversation")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(out.Text, "my-conversation") && !strings.Contains(out.Text, "saved") {
		t.Errorf("the save result does not say anything useful: %+v", out)
	}

	// A fresh session, as /load meets one restarted or pointed at another pane,
	// rather than the one /save just wrote from.
	d.withSession(tui.New(tui.Options{Model: "another/model"}))

	loadOut, err := d.load("my-conversation")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(loadOut.Text, "my-conversation") {
		t.Errorf("the load result does not name what was loaded: %+v", loadOut)
	}

	rows := d.session.Log().Rows()
	var texts []string
	for _, row := range rows {
		texts = append(texts, row.Text)
	}
	foundNotice, foundReply := false, false
	for _, text := range texts {
		if text == "first notice" {
			foundNotice = true
		}
		if text == "a reply" {
			foundReply = true
		}
	}
	if !foundNotice || !foundReply {
		t.Fatalf("the restored log is missing rows: %v", texts)
	}

	if got := d.session.Options().Model; got != "some/model" {
		t.Errorf("the model is %q, want the saved model %q", got, "some/model")
	}
}

// TestLoadReplacesRatherThanAppends covers Restore's own rule: a second /load,
// or a /load into a session that already had rows, leaves exactly the loaded
// rows behind rather than the loaded rows on top of what was there.
func TestLoadReplacesRatherThanAppends(t *testing.T) {
	d := saveDispatcher(t)

	d.session.Notice("to be saved", 0, tui.RoleNotice)
	if _, err := d.save("replace-me"); err != nil {
		t.Fatalf("save: %v", err)
	}

	d.session.Notice("this should not survive the load", 0, tui.RoleNotice)

	if _, err := d.load("replace-me"); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, row := range d.session.Log().Rows() {
		if row.Text == "this should not survive the load" {
			t.Fatalf("load appended to the log instead of replacing it: %+v",
				d.session.Log().Rows())
		}
	}
}

// TestSaveTwiceUnderTheSameNameOverwrites covers the store's own promise: a
// second /save under a name already used replaces the file rather than adding
// a second one, so /load always finds the latest conversation saved by that
// name.
func TestSaveTwiceUnderTheSameNameOverwrites(t *testing.T) {
	d := saveDispatcher(t)

	d.session.Notice("version one", 0, tui.RoleNotice)
	if _, err := d.save("same-name"); err != nil {
		t.Fatalf("save: %v", err)
	}

	d.withSession(tui.New(tui.Options{Model: "some/model"}))
	d.session.Notice("version two", 0, tui.RoleNotice)
	if _, err := d.save("same-name"); err != nil {
		t.Fatalf("second save: %v", err)
	}

	d.withSession(tui.New(tui.Options{Model: "some/model"}))
	if _, err := d.load("same-name"); err != nil {
		t.Fatalf("load: %v", err)
	}

	foundOne, foundTwo := false, false
	for _, row := range d.session.Log().Rows() {
		if row.Text == "version one" {
			foundOne = true
		}
		if row.Text == "version two" {
			foundTwo = true
		}
	}
	if foundOne {
		t.Error("the first save's row survived the second save")
	}
	if !foundTwo {
		t.Error("the second save's row is missing")
	}
}
