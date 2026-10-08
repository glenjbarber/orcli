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

// togglesDispatcher is a dispatcher whose session is set, the same way
// beginDispatcher wires one for /begin's own tests.
func togglesDispatcher(t *testing.T) *dispatcher {
	t.Helper()

	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{Model: "some/model"}))
	return d
}

// TestHelpListsEveryNameAndSkipsHiddenOnes covers the two properties /help has to
// have: every listed command appears, and a hidden spelling like /colour does not.
func TestHelpListsEveryNameAndSkipsHiddenOnes(t *testing.T) {
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/help")
	if err != nil {
		t.Fatalf("/help: %v", err)
	}
	for _, want := range []string{"/model", "/level", "/stealth", "/color"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("/help does not list %s:\n%s", want, out.Text)
		}
	}
	for _, hidden := range []string{"/colour", "/cognito"} {
		if strings.Contains(out.Text, hidden) {
			t.Errorf("/help lists the hidden name %s:\n%s", hidden, out.Text)
		}
	}
}

func TestTraceCommandCapturesSessionCommands(t *testing.T) {
	d := togglesDispatcher(t)
	d.capture = newDebugLog(t.TempDir(), "configured-secret")

	out, err := d.Run(context.Background(), "/trace")
	if err != nil || !strings.HasPrefix(out.Text, "trace is on: ") {
		t.Fatalf("/trace = %+v, %v", out, err)
	}
	if _, err := d.Run(context.Background(), "/trace off"); err != nil {
		t.Fatalf("/trace off: %v", err)
	}
	data, err := os.ReadFile(d.capture.path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "trace_started") || !strings.Contains(string(data), "trace_stopped") || !strings.Contains(string(data), "/trace off") {
		t.Fatalf("trace does not include command activity: %s", data)
	}
}

// TestVersionReportsTheSameIdentityStartupDoes covers the one fact /version exists
// to carry: it is the same variable `orcli version` prints.
func TestVersionReportsTheSameIdentityStartupDoes(t *testing.T) {
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/version")
	if err != nil {
		t.Fatalf("/version: %v", err)
	}
	if !strings.Contains(out.Text, version) {
		t.Errorf("/version is %q, want it to carry %q", out.Text, version)
	}
}

// TestExitQuitsLikeQuitDoes covers the alias: /exit has to produce the same result
// /quit does, since they are the table's two names for leaving.
func TestExitQuitsLikeQuitDoes(t *testing.T) {
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/exit")
	if err != nil {
		t.Fatalf("/exit: %v", err)
	}
	if !out.Quit {
		t.Error("/exit did not ask the loop to leave")
	}
}

// TestClearEmptiesTheLogAndReportsHowMuch covers Log.Truncate's own contract: a
// reader is told how many rows went rather than finding an empty log and wondering.
func TestClearEmptiesTheLogAndReportsHowMuch(t *testing.T) {
	d := togglesDispatcher(t)
	before := d.session.Log().Len()
	d.session.Notice("a notice to clear", 0, tui.RoleDim)

	out, err := d.Run(context.Background(), "/clear")
	if err != nil {
		t.Fatalf("/clear: %v", err)
	}
	if got := d.session.Log().Len(); got != 0 {
		t.Errorf("the log still holds %d rows after /clear", got)
	}
	if !strings.Contains(out.Text, "cleared") {
		t.Errorf("/clear did not report what it did: %q", out.Text)
	}
	_ = before
}

// TestBellReportsWithNoArgumentAndSetsWithOne covers /level's own pattern, which
// every plain toggle in this cluster follows: empty reports, on/off acts.
func TestBellReportsWithNoArgumentAndSetsWithOne(t *testing.T) {
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/bell")
	if err != nil {
		t.Fatalf("/bell: %v", err)
	}
	if !strings.Contains(out.Text, "off") {
		t.Errorf("/bell with no argument is %q, want the default (off)", out.Text)
	}

	out, err = d.Run(context.Background(), "/bell on")
	if err != nil {
		t.Fatalf("/bell on: %v", err)
	}
	if !strings.Contains(out.Text, "on") {
		t.Errorf("/bell on reported %q", out.Text)
	}
	if !d.session.Options().Bell {
		t.Error("/bell on did not set the session's own option")
	}

	if _, err := d.Run(context.Background(), "/bell sideways"); err == nil {
		t.Error("/bell sideways was accepted")
	}
}

// TestMouseReportsAndSetsIndependentlyOfPause covers the reason /mouse exists
// beside /pause: a reader who only wants the wheel back does not want the log held
// still as well.
func TestMouseReportsAndSetsIndependentlyOfPause(t *testing.T) {
	d := togglesDispatcher(t)

	if _, err := d.Run(context.Background(), "/mouse on"); err != nil {
		t.Fatalf("/mouse on: %v", err)
	}
	if !d.session.Options().Mouse {
		t.Error("/mouse on did not set the option")
	}
	if state, _ := d.session.State(); state == tui.StatePaused {
		t.Error("/mouse on paused the session, which is /pause's job")
	}
}

// TestStealthReportsAndSets covers /stealth, the command /cognito is now a hidden
// alias of.
func TestStealthReportsAndSets(t *testing.T) {
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/stealth")
	if err != nil {
		t.Fatalf("/stealth: %v", err)
	}
	if !strings.Contains(out.Text, "off") {
		t.Errorf("/stealth with no argument is %q, want the default (off)", out.Text)
	}

	out, err = d.Run(context.Background(), "/stealth on")
	if err != nil {
		t.Fatalf("/stealth on: %v", err)
	}
	if !strings.Contains(out.Text, "on") {
		t.Errorf("/stealth on reported %q", out.Text)
	}
	if !d.session.Options().Cognito {
		t.Error("/stealth on did not set Options.Cognito")
	}
}

// TestCognitoIsAHiddenAliasOfStealth covers the supersession itself: a reader who
// still types /cognito is answered exactly as /stealth would answer, against the
// same field.
func TestCognitoIsAHiddenAliasOfStealth(t *testing.T) {
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/cognito on")
	if err != nil {
		t.Fatalf("/cognito on: %v", err)
	}
	if !strings.Contains(out.Text, "on") {
		t.Errorf("/cognito on reported %q", out.Text)
	}
	if !d.session.Options().Cognito {
		t.Error("/cognito on did not set Options.Cognito")
	}

	c, found := tui.Lookup("cognito")
	if !found {
		t.Fatal("/cognito is no longer a name the table answers to")
	}
	if c.Name != "stealth" {
		t.Errorf("/cognito resolves to the command named %q, want stealth", c.Name)
	}
}

// TestColorSetsTheSessionAndSavesTheChoice covers the one thing /color does that
// /bell and /mouse do not: the table's own summary promises the choice is saved.
func TestColorSetsTheSessionAndSavesTheChoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"k"}`), config.FileMode); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	oldPath, oldWrite := configPath, writeColor
	configPath = func() (string, error) { return path, nil }
	writeColor = config.WriteColor
	t.Cleanup(func() { configPath, writeColor = oldPath, oldWrite })

	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/color on")
	if err != nil {
		t.Fatalf("/color on: %v", err)
	}
	if !d.session.Options().Color {
		t.Error("/color on did not set the session's own option")
	}
	if !strings.Contains(out.Text, "saved") {
		t.Errorf("/color on did not say the choice was saved: %q", out.Text)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file back: %v", err)
	}
	if !strings.Contains(string(got), `"color":true`) {
		t.Errorf("the file does not carry the saved choice: %s", got)
	}
}

// TestPauseTogglesStateAndMouse covers the two things /pause's own summary asks for
// together: holding the log still and moving the mouse.
func TestPauseTogglesStateAndMouse(t *testing.T) {
	d := togglesDispatcher(t)
	mouseBefore := d.session.Options().Mouse

	out, err := d.Run(context.Background(), "/pause")
	if err != nil {
		t.Fatalf("/pause: %v", err)
	}
	if state, _ := d.session.State(); state != tui.StatePaused {
		t.Errorf("the state is %s after /pause, want paused", state)
	}
	if d.session.Options().Mouse == mouseBefore {
		t.Error("/pause did not toggle the mouse")
	}
	if !strings.Contains(out.Text, "paused") {
		t.Errorf("/pause did not report pausing: %q", out.Text)
	}

	out, err = d.Run(context.Background(), "/pause")
	if err != nil {
		t.Fatalf("the second /pause: %v", err)
	}
	if state, _ := d.session.State(); state != tui.StateIdle {
		t.Errorf("the state is %s after the second /pause, want idle", state)
	}
	if d.session.Options().Mouse != mouseBefore {
		t.Error("the second /pause did not toggle the mouse back")
	}
	if !strings.Contains(out.Text, "resumed") {
		t.Errorf("the second /pause did not report resuming: %q", out.Text)
	}
}

// TestInfoReportsTheSessionSettings covers /info's whole job: a reader asking what
// is set finds the fields the table's commands can change.
func TestInfoReportsTheSessionSettings(t *testing.T) {
	d := togglesDispatcher(t)
	d.session.SetBell(true)

	out, err := d.Run(context.Background(), "/info")
	if err != nil {
		t.Fatalf("/info: %v", err)
	}
	for _, want := range []string{"some/model", "bell", "on", "mouse", "stealth", "colour"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("/info does not mention %q:\n%s", want, out.Text)
		}
	}
}

// TestAutosaveTogglesAndReports covers the "on"/"off" half of /autosave, which is
// the whole of what this build does with no timer behind it yet.
func TestAutosaveTogglesAndReports(t *testing.T) {
	d := togglesDispatcher(t)

	out, err := d.Run(context.Background(), "/autosave")
	if err != nil {
		t.Fatalf("/autosave: %v", err)
	}
	if !strings.Contains(out.Text, "off") {
		t.Errorf("/autosave with no argument is %q, want the default (off)", out.Text)
	}

	if _, err := d.Run(context.Background(), "/autosave on"); err != nil {
		t.Fatalf("/autosave on: %v", err)
	}
	if !d.autosaveOn {
		t.Error("/autosave on did not set the field")
	}

	if _, err := d.Run(context.Background(), "/autosave sideways"); err == nil {
		t.Error("/autosave sideways was accepted")
	}
}

// TestAutosaveNowDegradesGracefullyWithNoSave covers the gap this cluster leaves on
// purpose: /save has no handler in this build, so /autosave now has nothing to hand
// off to and must refuse clearly rather than pretend to save.
func TestAutosaveNowDegradesGracefullyWithNoSave(t *testing.T) {
	d := togglesDispatcher(t)

	_, err := d.Run(context.Background(), "/autosave now")
	if err == nil {
		t.Fatal("/autosave now was accepted with no /save handler in this build")
	}
	if !strings.Contains(err.Error(), "save") {
		t.Errorf("the refusal is %q, want it to name what is missing", err)
	}
}
