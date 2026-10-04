package tui

import (
	"fmt"
	"strings"
	"testing"
)

// TestARealFrameIsWhatAReaderSees is a diagnostic. It builds a session the way startup does,
// with a provider, a model and a credential, and prints the frame that comes out, so the
// layout is read off the code with the values a reader would actually be looking at.
//
// It exists because the other diagnostic fills the fields by hand, so it can only ever show
// the shape and not whether the values are the right length. A field column padded to the
// widest name is the whole of the alignment, and a provider that is longer than the widest
// name pushes the log right on its own row and nowhere else, which is exactly the fault a
// hand-filled diagnostic would not show.
func TestARealFrameIsWhatAReaderSees(t *testing.T) {
	screen, out := drawnAt(20, 80)

	s := New(Options{
		APIKey:     "sk-or-v1-a-key-the-frame-must-never-show",
		Model:      "stealth/space-bunny-alpha",
		Provider:   "openrouter.ai",
		Approval:   ApprovalAsk,
		WorkingDir: "/Users/gjb/Documents/github/orcli",
		Color:      true,
		Bell:       true,
	})

	l := &interfaceLoop{session: s, screen: screen}
	state, detail := s.State()

	var status Status
	status[fieldState] = string(state) + detailSuffix(detail)
	status[fieldCwd] = l.workingDir()
	status[fieldSession] = "Session 1"
	status[fieldProvider] = orNone(s.Options().Provider)
	status[fieldModel] = orNone(s.Options().Model)
	status[fieldKey] = present(s.Options().APIKey != "")
	status[fieldFigure] = "idle"
	status[fieldApproval] = orNone(string(s.Options().Approval))
	status[fieldVerbosity] = "0"
	status[fieldCognito] = onOff(s.Options().Cognito, "on", "off")
	status[fieldColor] = onOff(s.Options().Color, "on", "off")
	status[fieldMouse] = onOff(s.Options().Mouse, "on", "off")
	status[fieldCopy] = onOff(false, "available", "none")
	status[fieldBell] = onOff(s.Options().Bell, "on", "off")
	status[fieldPane] = "main"
	status[fieldWorkers] = plural(len(s.Workers()), "worker", "workers")
	status[fieldQueue] = plural(0, "prompt", "prompts")
	status[fieldHeld] = plural(s.Log().Len(), "row", "rows")
	status[fieldFolded] = plural(s.Log().Folded(), "row", "rows")
	status[fieldLevels] = plural(len(s.Levels()), "level", "levels")

	DrawStack(screen, stackLines(Bar{Status: status}, s.Log().Rows(), 20, plainPalette()),
		plainPalette())

	rows := stackPositionedRows(out.String())

	var b strings.Builder
	for i, line := range rows {
		fmt.Fprintf(&b, "%2d | %s\n", i+1, line)
	}
	t.Logf("a real frame on an 80 by 20 terminal:\n%s", b.String())

	// The credential is never written to a row. A frame row ends up in scrollback and a
	// reader selecting it gets the whole of it, so a key on the screen is a key in
	// whatever they pasted it into.
	if strings.Contains(out.String(), "sk-or-v1") {
		t.Error("the credential reached a frame row")
	}

	// Every log column is one column on every row. A row whose fields are longer than the
	// name column pushes its log right and the log below it is not under that one.
	at := nameWidth() + 1
	for i, line := range rows[:len(rows)-1] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if got := line[at:]; strings.HasPrefix(got, " ") && strings.TrimSpace(got) == "" {
			continue
		}
		if len(line) <= at {
			continue
		}
		if !strings.HasPrefix(line[at:], "") {
			t.Errorf("row %d does not carry a log at the one column", i+1)
		}
	}
}
