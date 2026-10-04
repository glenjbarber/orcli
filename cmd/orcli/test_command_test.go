package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/tui"
)

// TestTheTestCommandListsADirectory covers the handler itself, since a command a
// reader cannot reach is the state the table was in before this one.
//
// The listing is what a model reply is written to, so it is a single text the loop
// turns into a row, rather than a row per entry. That is the shape the assertion
// checks: newlines in the text, not new rows, since the loop owns the log.
func TestTheTestCommandListsADirectory(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zeta.txt", "alpha.md", "middle.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}

	d := newDispatcherFor(loadBody(t, `{"api_key":"k"}`))
	out, err := d.Run(context.Background(), "/test "+dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := "alpha.md\nmiddle.go\nsubdir/\nzeta.txt"
	if out.Text != want {
		t.Errorf("the listing is %q, want %q", out.Text, want)
	}
}

// TestTheListingIsSortedByName covers the reason it is sorted. A reader comparing two
// runs of the same command is comparing what is on disk, and an unsorted listing
// changes with the order the directory was written in, so a difference on screen would
// be a difference in the filesystem rather than in the frame.
func TestTheListingIsSortedByName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	d := newDispatcherFor(loadBody(t, `{"api_key":"k"}`))
	out, err := d.Run(context.Background(), "/test "+dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Text != "alpha\nbravo\ncharlie" {
		t.Errorf("the listing is %q, want it in name order", out.Text)
	}
}

// TestADirectoryIsMarkedAsOne covers the one thing a bare name cannot say, since a
// listing read on a terminal has nothing else to go on.
func TestADirectoryIsMarkedAsOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "inside"), 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	d := newDispatcherFor(loadBody(t, `{"api_key":"k"}`))
	out, err := d.Run(context.Background(), "/test "+dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "inside/") {
		t.Errorf("the directory is not marked: %q", out.Text)
	}
	if strings.Contains(out.Text, "file/") {
		t.Errorf("a file is marked as a directory: %q", out.Text)
	}
}

// TestAnEmptyDirectoryIsReportedAsEmpty covers the case where there is nothing wrong,
// since an empty result and a failed read look the same to a reader otherwise.
func TestAnEmptyDirectoryIsReportedAsEmpty(t *testing.T) {
	d := newDispatcherFor(loadBody(t, `{"api_key":"k"}`))
	out, err := d.Run(context.Background(), "/test "+t.TempDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "is empty") {
		t.Errorf("the reply is %q, want it to say the directory is empty", out.Text)
	}
}

// TestAMissingDirectoryIsARefusal covers the read that fails, which is the one case
// that is a result rather than text: a directory that is not there is a reader naming
// a path that does not exist, and an empty listing would say nothing was wrong.
func TestAMissingDirectoryIsARefusal(t *testing.T) {
	d := newDispatcherFor(loadBody(t, `{"api_key":"k"}`))

	_, err := d.Run(context.Background(), "/test "+filepath.Join(t.TempDir(), "nonesuch"))
	if err == nil {
		t.Fatal("a missing directory was listed as though it were there")
	}
	if !strings.Contains(err.Error(), "nonesuch") {
		t.Errorf("the refusal is %q, want it to name the path", err)
	}
}

// TestTheListingAsksNoModel covers the property that makes the command useful for a
// frame test. A reply that asked a model would be a reply whose rows the reader could
// not predict, and what moved on screen would be hard to attribute to the drawing.
func TestTheListingAsksNoModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	d := newDispatcherFor(loadBody(t, `{"api_key":"k"}`))
	d.canAsk = func() bool { return true }

	out, err := d.Run(context.Background(), "/test "+dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Ask != "" {
		t.Errorf("the listing asked the model: %q", out.Ask)
	}
	if out.Quit {
		t.Error("the listing asked the loop to leave")
	}
	if out.Text != "a" {
		t.Errorf("the listing is %q, want the one entry", out.Text)
	}
}

// TestTheCommandIsInTheTable covers the two places a name has to be: the table the
// help and the completer read, and the dispatcher's own map. A name in one and not the
// other is a command a reader can see in the help and is then told this build does not
// run.
func TestTheCommandIsInTheTable(t *testing.T) {
	c, listed := tui.Lookup("test")
	if !listed {
		t.Fatal("/test is not in the table, so the help and the completer do not offer it")
	}
	if c.Name != "test" {
		t.Errorf("the table entry is named %q, want test", c.Name)
	}
	if c.Summary == "" {
		t.Error("the table entry has no summary, so the help renders an empty line")
	}
	if got := tui.TestCommand().Name; got != "test" {
		t.Errorf("the exported entry is named %q, want test", got)
	}
}
