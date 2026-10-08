package main

import (
	"fmt"
	"strings"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// confirmModel records a model that the endpoint has answered a turn with.
//
// It is the last step of the connection, and there is no `/connect` command: the
// reader's own first question is the probe, and a reply arriving is what proves the
// credential and the model together. Nothing is written before that, since a model
// written on an unproven guess is a model the reader finds out about from a 400 on
// their next turn.
//
// The write goes through config.WriteModel rather than a write here, for the reason
// the file holds a credential: a writer outside internal/config is a second thing
// that can corrupt it, and the byte-level care of editing rather than re-encoding is
// what keeps the rest of the file as the reader wrote it.
//
// # A confirmation does not move last_model
//
// It writes the model without recording the one it replaced. The model being left is
// one the reader chose by hand, and a confirmation is not a reader choosing to move
// away from it. Recording it would make the reader's first question a move, and
// `/model last` would take them to a model they never chose rather than back to the
// one they had.
//
// A failure is reported to the reader as a row rather than swallowed, and the session
// carries on: the turn was answered and the answer is the thing they asked for, so a
// model that could not be written is a nuisance rather than a lost reply.
//
// It writes once per session rather than once per turn. The reader's first question
// is the probe, and after that the model is already in the file, so every later turn
// writing it again is a write the reader did not ask for.
//
// # It does not write when nothing changed
//
// Before writing, it reads the model already on disk with config.ReadModelPair and
// compares it to the one that just answered. A reader who already set up orcli and
// is simply using it again has the right model in the file before the first question
// is ever asked, and that first question answering is not that reader choosing
// anything: it is the common case, not the one this function exists for. A model
// written when nothing changed is a write the reader did not ask for and a file
// touched for no reason, so the notice that explains the write is skipped along with
// it. The once-per-session guard still sits on top of this check, so a session that
// has already written, or already found nothing to do, is not asked again on a later
// turn.
func confirmModel(s *tui.Session) func(string, bool) {
	written := false

	return func(model string, silent bool) {
		if written || model == "" {
			return
		}
		written = true

		// Unlike the HELO's own synthetic question, this notice is not silenced.
		// Glen confirmed (2026-10-07) that startup activity - this write included
		// - should be visible rather than leaving him wondering what happened, the
		// same reasoning that un-silenced the HELO's reply itself.
		notice := s.Notice

		path, err := configPath()
		if err != nil {
			notice(fmt.Sprintf("the model is %s, and the configuration file could not be found: %v",
				model, err), 0, tui.RoleFailure)
			return
		}

		if current, _, err := readModelPair(path); err == nil && current == model {
			return
		}

		if err := writeModel(path, model); err != nil {
			notice(fmt.Sprintf("the model is %s, and writing it failed: %v", model, err),
				0, tui.RoleFailure)
			return
		}

		// The reader is told where the model now lives, since a preference they did
		// not set by hand is one they would otherwise wonder where it came from.
		notice(fmt.Sprintf("the endpoint answered, so %s is written to %s", model, path),
			0, tui.RoleDim)
	}
}

// lastModel is the word `/model` reads as going back rather than choosing.
//
// It is a word rather than a model identifier, since a model identifier the endpoint
// offers is a path with a slash in it and this is not. It is a single word rather than
// a flag so the command keeps its shape: `/model NAME` chooses and `/model last` goes
// back, with nothing else to learn.
const lastModel = "last"

// modelHandler is the handler for `/model`.
//
// Four cases, and they are told apart before anything is written:
//
//   - no argument: report the model in force and the one `/model last` would reach.
//   - `last`: swap the two members, so the reader lands where they started. A second
//     `last` is the first undone.
//   - a name: record the model in force under last_model and write the new one.
//   - a name with nothing in force: write it and leave last_model alone, since there
//     is nothing to go back to.
//
// The session is told about the change as well as the file, since the frame draws the
// model and a command that wrote the file without changing what the session is
// answering with would leave the two disagreeing until the next turn.
func modelHandler(s *tui.Session, args string) (tui.Result, error) {
	path, err := configPath()
	if err != nil {
		return tui.Result{}, err
	}

	want := strings.TrimSpace(args)

	current, previous, err := config.ReadModelPair(path)
	if err != nil {
		return tui.Result{}, err
	}

	if want == "" {
		return tui.Result{Text: reportModel(current, previous)}, nil
	}

	if want == lastModel {
		// A `last` with nothing to go back to is refused by name rather than writing
		// an empty model, since an empty model is a session that cannot ask anything
		// and one the reader did not choose.
		if previous == "" {
			return tui.Result{Text: "there is no previous model to go back to"}, nil
		}

		// The swap is one write of the pair rather than two, since the two members are
		// one decision and a reader whose terminal died between two writes would be
		// left with a file that disagrees with itself. The no-op the reader named is
		// then a property of the pair rather than of the write: swapping twice puts
		// them back.
		if err := writeModelSwap(path, previous); err != nil {
			return tui.Result{}, err
		}
		s.SetModel(previous)

		return tui.Result{Text: fmt.Sprintf("the model is %s, and it was %s",
			previous, orNoneModel(current))}, nil
	}

	if err := writeModelSwap(path, want); err != nil {
		return tui.Result{}, err
	}
	s.SetModel(want)

	return tui.Result{Text: fmt.Sprintf("the model is %s, and it was %s",
		want, orNoneModel(current))}, nil
}

// reportModel is what `/model` with no argument prints.
//
// It names both members rather than only the one in force, since the whole of
// `/model last` is what the other one is, and a reader who cannot see it has to guess
// whether the command would do anything.
func reportModel(model, previous string) string {
	return fmt.Sprintf("the model is %s, and the one before it was %s",
		orNoneModel(model), orNoneModel(previous))
}

// orNoneModel renders an absent model as a dash.
//
// A field with no value is a dash rather than nothing, so a report does not shift
// shape as values arrive and a reader can see that the member is absent rather than
// wondering where it went.
func orNoneModel(model string) string {
	if model == "" {
		return "-"
	}
	return model
}

// configPath is where the confirmed model is written.
//
// It is a variable rather than a call to config.DefaultPath so a test can point the
// confirmation at a temporary file. Without that seam a test exercising a confirmed
// turn writes to the reader's own configuration, which is the one side effect in this
// program a test must never have.
var configPath = config.DefaultPath

// readModelPair is the configuration reader, as a variable rather than a call.
//
// It is the seam confirmModel uses to find out what is already on disk before
// deciding whether a write is needed, and it is what lets a test prove that a
// confirmed turn matching the file on disk writes nothing.
var readModelPair = config.ReadModelPair

// writeModel is the configuration writer, as a variable rather than a call.
//
// It is a seam for the same reason as the four in main.go, and it is what lets a test
// prove that a confirmed turn writes the model without a file being written.
var writeModel = config.WriteModel

// writeModelSwap is the configuration writer that sets the model and records the one
// being replaced.
//
// It is a second seam rather than a field on the first because it is a second
// operation, and a test that proved a swap needs to stand in for the swap rather than
// for the plain write. The pair is read inside it, so a caller hands over only the
// model it wants in force.
var writeModelSwap = config.WriteModelSwap
