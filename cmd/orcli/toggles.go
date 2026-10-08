package main

import (
	"fmt"
	"strings"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// help is the handler for /help.
//
// It lists every name the completer would offer, which is Names rather than the raw
// table: a hidden spelling like /colour is accepted when typed and is not something
// a reader asking what this program can do should be told about, since listing it
// would spend a line on a spelling nobody asked for. The names are already
// alphabetical, which is the order a reader scans a list in rather than the order
// they were declared in the table.
func (d *dispatcher) help() (tui.Result, error) {
	names := tui.Names()

	var b strings.Builder
	for i, name := range names {
		c, found := tui.Lookup(name)
		if !found {
			// Names only returns what byName built from the table, so a miss here
			// is a bug in that table rather than something a reader typed.
			continue
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "/%s", name)
		if c.Args != "" {
			fmt.Fprintf(&b, " %s", c.Args)
		}
		fmt.Fprintf(&b, "  %s", c.Summary)
	}
	return tui.Result{Text: b.String()}, nil
}

// version is the handler for /version.
//
// It reports the same identity `orcli --version` and `orcli version` print at
// startup, read off the one variable both of those and this share, so a reader
// comparing what a running session answers against what the binary on disk reports
// is comparing the same figure rather than two that can drift apart.
func (d *dispatcher) version() (tui.Result, error) {
	return tui.Result{Text: fmt.Sprintf("orcli %s", version)}, nil
}

// clear is the handler for /clear.
//
// The count is reported rather than left silent, on the same grounds Log.Truncate's
// own doc comment gives: a command that removed rows and said nothing about how many
// would look like a quiet failure, and the figure costs nothing to read for a reader
// who does not care what it says.
func (d *dispatcher) clear() (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/clear needs an open interface")
	}

	log := d.session.Log()
	n := log.Len()
	log.Truncate()
	return tui.Result{Text: fmt.Sprintf("cleared %d rows", n)}, nil
}

// onOff parses the one argument every plain toggle in this file takes.
//
// Three outcomes rather than two: empty is "say nothing new", a recognised word is a
// decision, and anything else is a typo the reader should be told about by name
// rather than have silently read as one of the two words it is not.
func onOff(args string) (set bool, on bool, err error) {
	switch strings.TrimSpace(args) {
	case "":
		return false, false, nil
	case "on":
		return true, true, nil
	case "off":
		return true, false, nil
	default:
		return false, false, fmt.Errorf("must be on or off, got %q", strings.TrimSpace(args))
	}
}

// reportOnOff renders a toggle's state the way /level and /model report theirs: in
// a sentence rather than the bare word, so a reader who types the command cold
// without the args hint learns which toggle they just read.
func reportOnOff(name string, on bool) string {
	return fmt.Sprintf("%s is %s", name, onOffWord(on))
}

// onOffWord is the word a toggle's state is written as.
func onOffWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// bell is the handler for /bell.
//
// It mirrors /level's shape: no argument reports the state already in force rather
// than changing it, and the only two words that act on it are the ones the
// configuration file itself would write. There is nothing to persist here, unlike
// /color: the table's own summary for /bell makes no promise about surviving a
// restart, and the flag at startup (`--bell`) is where a reader fixes it for good.
func (d *dispatcher) bell(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/bell needs an open interface")
	}

	set, on, err := onOff(args)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/bell %w", err)
	}
	if !set {
		return tui.Result{Text: reportOnOff("the bell", d.session.Options().Bell)}, nil
	}

	d.session.SetBell(on)
	return tui.Result{Text: reportOnOff("the bell", on)}, nil
}

// mouse is the handler for /mouse.
//
// It is the same shape as /bell, and it is deliberately not the thing /pause moves
// on its own: a reader who only ever wants the wheel scrolling again, with no turn
// held still, types this rather than /pause, and a /mouse that routed through /pause
// would hold the log still for a reader who asked for neither.
func (d *dispatcher) mouse(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/mouse needs an open interface")
	}

	set, on, err := onOff(args)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/mouse %w", err)
	}
	if !set {
		return tui.Result{Text: reportOnOff("the mouse", d.session.Options().Mouse)}, nil
	}

	d.session.SetMouse(on)
	return tui.Result{Text: reportOnOff("the mouse", on)}, nil
}

// copymode is the handler for /copymode.
//
// It is the same shape as /bell and /mouse: no argument reports the state already
// in force, and `on`/`off` moves it. There is nothing to persist, the same as
// /bell - a reader turns it on for one terminal-native selection and off again
// right after, not for good, so it is not a flag at startup either.
//
// While it is on, the footer's stat fields and the twiddle sweep freeze at their
// last-computed values - see Options.CopyMode's own doc comment in internal/tui for
// what exactly stops moving and why - so a reader can click-drag a selection in
// their terminal without the screen repainting under it. The log itself keeps
// rendering new rows as usual; only the footer's noise is held still.
func (d *dispatcher) copymode(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/copymode needs an open interface")
	}

	set, on, err := onOff(args)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/copymode %w", err)
	}
	if !set {
		return tui.Result{Text: reportOnOff("copy mode", d.session.Options().CopyMode)}, nil
	}

	d.session.SetCopyMode(on)
	return tui.Result{Text: reportOnOff("copy mode", on)}, nil
}

// stealth is the handler for /stealth, and for /cognito, which is now a hidden
// alias of it rather than a command of its own.
//
// The rename is Glen's, confirmed in this session on 2026-10-07: /cognito is
// superseded by /stealth as the listed, completed and documented name for the same
// toggle, over the same Options.Cognito field, and /cognito keeps answering so
// existing muscle memory still works. See internal/tui/command.go's table, where
// /cognito is now one of /stealth's Hidden names in the same way /colour is one of
// /color's.
//
// The report is worded from the table's own summary ("record nothing") rather than
// from the field name, since a reader who has never used this and asks it what it
// means with no argument should be told what not recording means rather than be
// handed the on/off word with nothing to attach it to.
func (d *dispatcher) stealth(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/stealth needs an open interface")
	}

	set, on, err := onOff(args)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/stealth %w", err)
	}
	if !set {
		on := d.session.Options().Cognito
		if on {
			return tui.Result{Text: "stealth is on: nothing is to be recorded"}, nil
		}
		return tui.Result{Text: "stealth is off: this session records as usual"}, nil
	}

	d.session.SetCognito(on)
	if on {
		return tui.Result{Text: "stealth is on: nothing is to be recorded"}, nil
	}
	return tui.Result{Text: "stealth is off: this session records as usual"}, nil
}

// color is the handler for /color.
//
// Unlike /bell and /mouse, the table's own summary promises the choice is saved, so
// a decision here moves two things rather than one: the session's own copy, which
// is what the status bar and the palette read starting with the next line drawn, and
// the configuration file, through config.WriteColor, which is the byte-level writer
// that keeps the rest of a hand-edited file intact. The session is told whether or
// not the choice is saved, since a session with no configuration file to write to
// should not leave a reader believing it is written when it is not.
func (d *dispatcher) color(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/color needs an open interface")
	}

	set, on, err := onOff(args)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/color %w", err)
	}
	if !set {
		return tui.Result{Text: reportOnOff("colour", d.session.Options().Color)}, nil
	}

	d.session.SetColor(on)

	path, err := configPath()
	if err != nil {
		return tui.Result{Text: fmt.Sprintf(
			"colour is %s, and the configuration file could not be found so the choice is not saved: %v",
			onOffWord(on), err)}, nil
	}
	if err := writeColor(path, on); err != nil {
		return tui.Result{Text: fmt.Sprintf(
			"colour is %s, and saving the choice to %s failed: %v", onOffWord(on), path, err)}, nil
	}

	return tui.Result{Text: fmt.Sprintf("colour is %s, saved to %s", onOffWord(on), path)}, nil
}

// writeColor is the configuration writer /color saves through, as a variable
// rather than a call for the reason writeModel is one: a test proving /color saves
// the choice without touching a reader's real file stands in for this.
var writeColor = config.WriteColor

// pause is the handler for /pause.
//
// The table's summary is two actions in one word: stop what the client writes, and
// toggle the mouse. There is no third state to hold "what to resume to" in, so the
// toggle is between StatePaused and StateIdle rather than between StatePaused and
// whatever the session was doing before: a reader who pauses mid-turn and then
// un-pauses lands idle rather than back in the state the turn was in, which is a
// genuine open question about exactly what "stop what the client writes" should
// restore, noted as a gap in the PR rather than guessed at silently.
func (d *dispatcher) pause() (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/pause needs an open interface")
	}

	state, _ := d.session.State()
	mouse := d.session.Options().Mouse

	if state == tui.StatePaused {
		d.session.SetState(tui.StateIdle, "")
		d.session.SetMouse(!mouse)
		return tui.Result{Text: fmt.Sprintf("resumed, %s", reportOnOff("the mouse", !mouse))}, nil
	}

	d.session.SetState(tui.StatePaused, "")
	d.session.SetMouse(!mouse)
	return tui.Result{Text: fmt.Sprintf("paused, %s", reportOnOff("the mouse", !mouse))}, nil
}

// info is the handler for /info.
//
// It is printSession's own report, carried into a running session rather than
// written to stdout at startup: the two read the same fields from two different
// places because the startup report has a session value built from flags and a
// configuration file and this one has a *tui.Session already open, and there is no
// single value both would take without one of them reaching past what it owns.
func (d *dispatcher) info() (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/info needs an open interface")
	}

	opts := d.session.Options()

	var b strings.Builder
	fmt.Fprintf(&b, "model          %s\n", orNone(opts.Model))
	fmt.Fprintf(&b, "provider       %s\n", orNone(opts.Provider))
	fmt.Fprintf(&b, "approval       %s\n", orNone(string(opts.Approval)))
	fmt.Fprintf(&b, "mouse          %s\n", enabled(opts.Mouse))
	fmt.Fprintf(&b, "bell           %s\n", enabled(opts.Bell))
	fmt.Fprintf(&b, "stealth        %s\n", enabled(opts.Cognito))
	fmt.Fprintf(&b, "colour         %s\n", enabled(opts.Color))
	fmt.Fprintf(&b, "verbosity      %d\n", opts.Verbosity)
	fmt.Fprintf(&b, "level          %s\n", orNone(d.session.Preset()))
	fmt.Fprintf(&b, "break          every %s, bell %s\n",
		opts.BreakInterval, enabled(opts.BreakBell))
	fmt.Fprintf(&b, "working dir    %s", orNone(opts.WorkingDir))

	return tui.Result{Text: b.String()}, nil
}

// autosave is the handler for /autosave.
//
// Only the toggle exists in this build. There is no timer anywhere in this tree
// that writes a session to disk on its own, so "on" and "off" move d.autosaveOn and
// nothing else yet, and `/autosave now` degrades to a clear refusal rather than a
// save, since /save itself is not wired into the dispatcher's table as of this
// commit (see the PR this shipped in). A later change that builds /save's handler
// and a timer driven by d.autosaveOn is what "now" is written to grow into, not a
// second thing this command invents for itself.
func (d *dispatcher) autosave(args string) (tui.Result, error) {
	want := strings.TrimSpace(args)

	switch want {
	case "":
		return tui.Result{Text: reportOnOff("autosave", d.autosaveOn)}, nil
	case "on":
		d.autosaveOn = true
		if d.session != nil {
			d.session.SetAutosave(true)
		}
		return tui.Result{Text: reportOnOff("autosave", true)}, nil
	case "off":
		d.autosaveOn = false
		if d.session != nil {
			d.session.SetAutosave(false)
		}
		return tui.Result{Text: reportOnOff("autosave", false)}, nil
	case "now":
		// /save has no handler in d.commands yet (see the separate save/load
		// cluster), so there is nothing this can hand a conversation to. Refusing
		// by name is better than a save that silently did nothing, or one that
		// reached into internal/saved on its own and wrote a file /load would
		// have no command to read back.
		return tui.Result{}, fmt.Errorf(
			"/autosave now: /save is not wired into this build yet, so there is nothing to save to")
	default:
		return tui.Result{}, fmt.Errorf("/autosave %q is not on, off or now", want)
	}
}
