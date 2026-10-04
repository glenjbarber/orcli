package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// overModelAt points the three model seams at a file, and returns a dispatcher over a
// session carrying the model the session was built with.
//
// All three are pointed rather than one, since the handler reads the pair out of the
// file before it decides anything and a test that pointed only the writer would be
// reading the reader's own configuration to decide what the swap would do.
func overModelAt(t *testing.T, path string, sessionModel string) *dispatcher {
	t.Helper()

	oldPath, oldWrite, oldSwap := configPath, writeModel, writeModelSwap
	configPath = func() (string, error) { return path, nil }
	writeModel = config.WriteModel
	writeModelSwap = config.WriteModelSwap

	t.Cleanup(func() {
		configPath, writeModel, writeModelSwap = oldPath, oldWrite, oldSwap
	})

	s := tui.New(tui.Options{Model: sessionModel, APIKey: "k"})
	d := newDispatcherFor(config.Default())
	d.withSession(s)
	return d
}

// overModel writes a configuration and returns a dispatcher over it, along with the
// session and the path.
func overModel(t *testing.T, body string) (*dispatcher, *tui.Session, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(body), config.FileMode); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	return overModelAt(t, path, "first/model"), nil, path
}

// pair returns the two members as the file has them.
func pair(t *testing.T, path string) (model, last string) {
	t.Helper()

	model, last, err := config.ReadModelPair(path)
	if err != nil {
		t.Fatalf("read the pair: %v", err)
	}
	return model, last
}

// runModel runs the handler and fails the test on an error.
func runModel(t *testing.T, d *dispatcher, args string) tui.Result {
	t.Helper()

	out, err := d.Run(context.Background(), "/model "+args)
	if err != nil {
		t.Fatalf("/model %s: %v", args, err)
	}
	return out
}

// readAll returns what is at path.
func readAll(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestChoosingAModelRecordsTheOneItReplaces is the whole of `/model NAME`: the reader
// gets the model they asked for and a way back to the one they had.
func TestChoosingAModelRecordsTheOneItReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"k","model":"first/model"}`), config.FileMode); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d := overModelAt(t, path, "first/model")
	s := d.session

	runModel(t, d, "second/model")

	model, last := pair(t, path)
	if model != "second/model" {
		t.Errorf("the model is %q, want the one chosen", model)
	}
	if last != "first/model" {
		t.Errorf("last_model is %q, want the one replaced", last)
	}
	if got := s.Options().Model; got != "second/model" {
		t.Errorf("the session answers with %q, want the one chosen", got)
	}
}

// TestLastGoesBack is the command the reader named. One swap is where the reader was
// before, and that is what makes pressing it twice safe.
func TestLastGoesBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path,
		[]byte(`{"api_key":"k","model":"second/model","last_model":"first/model"}`),
		config.FileMode); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d := overModelAt(t, path, "second/model")

	runModel(t, d, "last")

	model, last := pair(t, path)
	if model != "first/model" {
		t.Errorf("the model is %q, want the one before", model)
	}
	if last != "second/model" {
		t.Errorf("last_model is %q, want the one swapped with", last)
	}
	if got := d.session.Options().Model; got != "first/model" {
		t.Errorf("the session answers with %q, want the one before", got)
	}
}

// TestLastTwiceIsANoOp is the property the reader named exactly: two swaps put the
// members back where they were. The file is written twice and the pair is unchanged,
// which is what a reader pressing it twice gets.
func TestLastTwiceIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path,
		[]byte(`{"api_key":"k","model":"second/model","last_model":"first/model"}`),
		config.FileMode); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d := overModelAt(t, path, "second/model")

	before := readAll(t, path)

	runModel(t, d, "last")
	runModel(t, d, "last")

	after := readAll(t, path)

	model, last := pair(t, path)
	if model != "second/model" || last != "first/model" {
		t.Errorf("two swaps gave model %q and last %q, want the pair unchanged", model, last)
	}
	if after != before {
		t.Errorf("two swaps changed the file:\nbefore %s\nafter  %s", before, after)
	}
}

// TestLastWithNothingToGoBackToIsRefusedByName covers the state a reader is in before
// they have chosen anything. An empty model is a session that cannot ask, and one the
// reader did not choose.
func TestLastWithNothingToGoBackToIsRefusedByName(t *testing.T) {
	d, _, path := overModel(t, `{"api_key":"k"}`)

	out := runModel(t, d, "last")

	if !strings.Contains(out.Text, "no previous model") {
		t.Errorf("the reply is %q, want it to say there is nothing to go back to", out.Text)
	}
	if model, last := pair(t, path); model != "" || last != "" {
		t.Errorf("the file changed: model %q and last %q", model, last)
	}
}

// TestChoosingWithNothingInForceLeavesNoLast covers the first choice a reader makes.
// There is no model to go back to, so recording one would write a member meaning
// nothing.
func TestChoosingWithNothingInForceLeavesNoLast(t *testing.T) {
	d, _, path := overModel(t, `{"api_key":"k"}`)

	runModel(t, d, "only/model")

	model, last := pair(t, path)
	if model != "only/model" {
		t.Errorf("the model is %q, want the one chosen", model)
	}
	if last != "" {
		t.Errorf("last_model is %q, want nothing recorded", last)
	}
}

// TestModelWithNoArgumentReportsBoth covers the listing. The whole of `/model last` is
// what the other member is, and a reader who cannot see it has to guess whether the
// command would do anything.
func TestModelWithNoArgumentReportsBoth(t *testing.T) {
	d, _, _ := overModel(t, `{"api_key":"k","model":"second/model","last_model":"first/model"}`)

	out := runModel(t, d, "")

	for _, want := range []string{"second/model", "first/model"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the report does not carry %q: %q", want, out.Text)
		}
	}
}

// TestAChoiceKeepsTheCredentialAndTheSingleLineShape covers what every writer in that
// package works to preserve. The file holds a credential and the reader may have
// written it on one line.
func TestAChoiceKeepsTheCredentialAndTheSingleLineShape(t *testing.T) {
	d, _, path := overModel(t, `{"api_key":"k","model":"first/model"}`)

	runModel(t, d, "second/model")

	got := readAll(t, path)
	if strings.Contains(got, "\n") {
		t.Errorf("a single-line file came back over several lines: %q", got)
	}
	if !strings.Contains(got, `"api_key":"k"`) {
		t.Errorf("the credential did not survive:\n%s", got)
	}
	key, model := strings.Index(got, `"api_key"`), strings.Index(got, `"model"`)
	if key < 0 || model < 0 {
		t.Fatalf("a member went missing:\n%s", got)
	}
	if key > model {
		t.Errorf("the members were reordered:\n%s", got)
	}
}

// TestTheModelMemberIsReadFromTheFileNotTheSession covers the source of truth. A reader
// who edited their own configuration by hand is answered with the model their file says
// they wrote, and the session value is whatever startup read before they edited it.
func TestTheModelMemberIsReadFromTheFileNotTheSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path,
		[]byte(`{"api_key":"k","model":"edited/model","last_model":"older/model"}`),
		config.FileMode); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	d := overModelAt(t, path, "stale/model")

	out := runModel(t, d, "")

	if !strings.Contains(out.Text, "edited/model") {
		t.Errorf("the report is %q, want the model the file holds", out.Text)
	}
}
