package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// standDrawFor replaces the interface-opening call for the duration of one test.
//
// The seam is a variable for the same reason the gate is one: a test that cannot
// stand in for the call into a terminal can only run on a machine with a terminal
// attached, and the wiring is exactly the thing that has no other way to be checked.
func standDrawFor(t *testing.T, fn func(context.Context, *tui.Session, config.Config,
	io.Reader, io.Writer) error) {
	t.Helper()

	restore := draw
	t.Cleanup(func() { draw = restore })
	draw = fn
}

// standTerminalFor answers the terminal check as the test wants it answered.
//
// It is a seam for the same reason draw is one, and it is a second seam rather than a
// folding of the first because the two answer different questions: this one asks
// whether the streams are terminals at all, and openInterface asks whether there is a
// descriptor and a size to draw on. A test that stood in for both would not be
// testing that the interface opens when the reader is at a terminal, since the whole
// condition would be the stand-in.
func standTerminalFor(t *testing.T, answer bool) {
	t.Helper()

	restore := tuiStreamsAreTerminal
	t.Cleanup(func() { tuiStreamsAreTerminal = restore })
	tuiStreamsAreTerminal = func(io.Reader, io.Writer) bool { return answer }
}

// TestTheInterfaceIsOpenedAtATerminal covers the call itself, since a tui.Start with no
// caller is the state this wiring exists to leave.
func TestTheInterfaceIsOpenedAtATerminal(t *testing.T) {
	standTerminalFor(t, true)

	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		called := false
		standDrawFor(t, func(context.Context, *tui.Session, config.Config,
			io.Reader, io.Writer) error {
			called = true
			return nil
		})

		_, _, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}

		if !called {
			t.Error("the interface was not opened on a run whose streams are terminals")
		}
	})
}

// TestTheInterfaceIsNotOpenedOnARedirectedRun is the case the terminal check exists
// for. A redirected interface writes escape sequences into whatever is reading, and
// the reader gets noise rather than a report.
func TestTheInterfaceIsNotOpenedOnARedirectedRun(t *testing.T) {
	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		called := false
		standDrawFor(t, func(context.Context, *tui.Session, config.Config,
			io.Reader, io.Writer) error {
			called = true
			return nil
		})

		_, stderr, err := runIn(t, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}

		if called {
			t.Error("the interface was opened on a run whose streams are buffers")
		}
		if !strings.Contains(stderr, "terminals") {
			t.Errorf("a redirected run was not told why: %q", stderr)
		}
	})
}

// TestAFailureOpeningTheInterfaceStopsStartup covers the error path, since a draw
// that reported nothing would leave a reader with a session report and no frame and no
// fault to explain the gap.
func TestAFailureOpeningTheInterfaceStopsStartup(t *testing.T) {
	standTerminalFor(t, true)

	withHome(t, func() {
		writeConfig(t, `{"api_key":"k"}`)

		standDrawFor(t, func(context.Context, *tui.Session, config.Config,
			io.Reader, io.Writer) error {
			return io.ErrUnexpectedEOF
		})

		stdout, _, err := runIn(t, false)
		if err == nil {
			t.Fatal("startup continued past a failure to open the interface")
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("the failure is %v, want the one the frame reported", err)
		}
		if strings.Contains(stdout, "configuration") {
			t.Error("a session report was printed after the frame failed")
		}
	})
}

// TestTheInterfaceIsToldTheStreamCannotBeAsked is the refusal openInterface makes for
// a stream it has no descriptor for.
//
// The check is a type assertion rather than a size query, since a size query needs a
// descriptor and a buffer is not one. A reader who pipes this gets the reason rather
// than a draw into the pipe.
func TestTheInterfaceIsToldTheStreamCannotBeAsked(t *testing.T) {
	var b strings.Builder

	err := openInterface(context.Background(), nil, config.Config{}, strings.NewReader(""), &b)
	if !errors.Is(err, tui.ErrNoTerminal) {
		t.Errorf("a buffer was given to openInterface and it returned %v, want ErrNoTerminal", err)
	}
}

// TestTheTranslationCarriesTheResolvedSettings covers the part of the wiring with the
// most room to be wrong, since it is a field-by-field copy from one struct into
// another and a field left out is a setting a reader wrote that the frame ignores.
func TestTheTranslationCarriesTheResolvedSettings(t *testing.T) {
	s := session{
		Config: config.Config{
			APIKey:   "k",
			Model:    "some/model",
			Provider: "example.test",
		},
		Approval:   config.ApprovalDeny,
		WorkingDir: "/tmp/where",
		Color:      true,
		Bell:       true,
		Mouse:      true,
	}

	opts := s.tuiSession().Options()

	if opts.APIKey != "k" {
		t.Errorf("the credential arrived as %q", opts.APIKey)
	}
	if opts.Model != "some/model" {
		t.Errorf("the model arrived as %q", opts.Model)
	}
	if opts.Provider != "example.test" {
		t.Errorf("the provider arrived as %q", opts.Provider)
	}
	if opts.Approval != tui.ApprovalDeny {
		t.Errorf("the approval arrived as %q", opts.Approval)
	}
	if opts.WorkingDir != "/tmp/where" {
		t.Errorf("the working directory arrived as %q", opts.WorkingDir)
	}
	for name, on := range map[string]bool{
		"color": opts.Color, "bell": opts.Bell, "mouse": opts.Mouse,
	} {
		if !on {
			t.Errorf("%s was lost in the translation", name)
		}
	}
}

// TestTheNoRecordingModeIsNotInvented covers the one field the translation leaves at
// its default, and the reason is the rule rather than an omission.
//
// The cognito marker is a file beside the configuration rather than a member of it,
// so there is no config.Config field to copy from. A translation that reached past
// the configuration for the mode would be inventing a decision the reader has not
// made, and this test says the field stays off until something reads the marker.
func TestTheNoRecordingModeIsNotInvented(t *testing.T) {
	s := session{Config: config.Config{APIKey: "k"}, Approval: config.ApprovalAsk}

	if s.tuiSession().Options().Cognito {
		t.Error("the no-recording mode arrived as on, and nothing has read the marker")
	}
}

// TestAnUnknownApprovalModeBecomesAsk covers the fallthrough, since a mode this build
// has not heard of must not arrive at the bars as a blank field.
func TestAnUnknownApprovalModeBecomesAsk(t *testing.T) {
	for _, mode := range []config.Approval{config.ApprovalAsk, config.Approval("sometimes")} {
		if got := tuiApproval(mode); got != tui.ApprovalAsk {
			t.Errorf("approval %q arrived as %q, want ask", mode, got)
		}
	}
}

// TestTheApprovalModesAreCarriedAsWritten covers the other two, which are the cases
// where a conversion that mapped everything to ask would pass the test above.
func TestTheApprovalModesAreCarriedAsWritten(t *testing.T) {
	if got := tuiApproval(config.ApprovalAllow); got != tui.ApprovalAllow {
		t.Errorf("allow arrived as %q", got)
	}
	if got := tuiApproval(config.ApprovalDeny); got != tui.ApprovalDeny {
		t.Errorf("deny arrived as %q", got)
	}
}

// TestATurnCarriesTheAttribution covers the field the wiring added, since a reply that
// is not attributed to whoever is holding the session is a reply the provider cannot
// bill or record to anyone.
func TestATurnCarriesTheAttribution(t *testing.T) {
	s := session{
		Config: config.Config{
			APIKey:        "k",
			Model:         "some/model",
			AttributionID: "who",
		},
		Approval: config.ApprovalAsk,
	}

	// The attribution is not carried into the interface: it belongs to the request,
	// and a session holding it is a session holding a second thing a reader never
	// sees and never uses.
	if got := s.tuiSession().Options().APIKey; got != "k" {
		t.Errorf("the credential arrived as %q", got)
	}
}
