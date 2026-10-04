package main

import (
	"fmt"

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
// A failure is reported to the reader as a row rather than swallowed, and the session
// carries on: the turn was answered and the answer is the thing they asked for, so a
// model that could not be written is a nuisance rather than a lost reply.
//
// It writes once per session rather than once per turn. The reader's first question
// is the probe, and after that the model is already in the file, so every later turn
// writing it again is a write the reader did not ask for.
func confirmModel(s *tui.Session) func(string) {
	written := false

	return func(model string) {
		if written || model == "" {
			return
		}
		written = true

		path, err := configPath()
		if err != nil {
			s.Notice(fmt.Sprintf("the model is %s, and the configuration file could not be found: %v",
				model, err), 0, tui.RoleFailure)
			return
		}

		if err := writeModel(path, model); err != nil {
			s.Notice(fmt.Sprintf("the model is %s, and writing it failed: %v", model, err),
				0, tui.RoleFailure)
			return
		}

		// The reader is told where the model now lives, since a preference they did
		// not set by hand is one they would otherwise wonder where it came from.
		s.Notice(fmt.Sprintf("the endpoint answered, so %s is written to %s", model, path),
			0, tui.RoleDim)
	}
}

// configPath is where the confirmed model is written.
//
// It is a variable rather than a call to config.DefaultPath so a test can point the
// confirmation at a temporary file. Without that seam a test exercising a confirmed
// turn writes to the reader's own configuration, which is the one side effect in this
// program a test must never have.
var configPath = config.DefaultPath

// writeModel is the configuration writer, as a variable rather than a call.
//
// It is a seam for the same reason as the four in main.go, and it is what lets a
// test prove that a confirmed turn writes the model without a file being written.
var writeModel = config.WriteModel
