package tui

import (
	"context"
	"strings"
	"testing"
)

// TestStartSendsHelloAutomatically covers the startup hello at the level Start's
// own event loop runs it at, without opening a real terminal screen: a test binary
// has none, and driving tview's own Application through a fake screen is more than
// this hook needs proving. interfaceLoop.start is the exact call Start makes for a
// non-empty hello, on the same group a reader's own line would be started on, so
// calling it directly here exercises the real mechanism - the goroutine, the
// group accounting, and the session writes - with no key pressed and no line
// runner consulted at all.
func TestStartSendsHelloAutomatically(t *testing.T) {
	s := New(Options{Model: "some/model"})

	var asked string
	var gotSilent bool
	ask := func(_ context.Context, question string, level int, silent bool) error {
		asked = question
		gotSilent = silent
		s.Deliver("hello back", level)
		return nil
	}

	l := &interfaceLoop{session: s, ask: ask, group: newGroup()}
	maybeSendHello(context.Background(), l, "introduce yourself")
	if err := l.group.Close(); err != nil {
		t.Fatalf("group.Close: %v", err)
	}

	if asked != "introduce yourself" {
		t.Errorf("ask was called with %q, want the hello text", asked)
	}
	if !gotSilent {
		t.Error("ask was called with silent = false, want the hello sent silent so its own text never becomes a log row")
	}

	rows := s.Log().Rows()
	if got := rows[len(rows)-1].Text; got != "hello back" {
		t.Errorf("last log row = %q, want the hello's own reply, written with no line submitted", got)
	}
	for _, row := range rows {
		if row.Text == "introduce yourself" {
			t.Errorf("the hello's own question text appeared as a log row: %+v, want only its reply visible", row)
		}
	}
}

// TestStartSendsNoHelloWhenEmpty covers the other side: an empty hello is a
// caller's choice not to greet, and maybeSendHello - the exact call Start makes -
// starts no turn at all when it is given one.
func TestStartSendsNoHelloWhenEmpty(t *testing.T) {
	s := New(Options{Model: "some/model"})

	called := false
	ask := func(context.Context, string, int, bool) error {
		called = true
		return nil
	}

	l := &interfaceLoop{session: s, ask: ask, group: newGroup()}
	maybeSendHello(context.Background(), l, "")
	_ = l.group.Close()

	if called {
		t.Error("ask was called despite an empty hello")
	}
}

// TestMaybeSendHelloSkipsWithNoAsk covers a loop built with no ask at all - a
// session with no model configured, say - sending nothing rather than calling a
// nil function.
func TestMaybeSendHelloSkipsWithNoAsk(t *testing.T) {
	s := New(Options{Model: "some/model"})
	l := &interfaceLoop{session: s, group: newGroup()}

	maybeSendHello(context.Background(), l, "introduce yourself")
	_ = l.group.Close()
}

func plainPalette() Palette { return NewPalette(false, GroundDark, nil) }

func TestTheBottomBarCarriesIdentityAndApproval(t *testing.T) {
	got := RenderBottom("Session 1", true, false,
		"openrouter.ai", "stealth/space-bunny-alpha", "3", "ask")
	for _, want := range []string{"Session 1", "[Mouse]", "Provider: openrouter.ai", "Model: stealth/space-bunny-alpha", "Approval: ask", "Verbosity: 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("bottom bar does not carry %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Mouse:") || strings.Index(got, "Approval") > strings.Index(got, "Verbosity") {
		t.Errorf("bottom bar has an invalid field order or mouse label: %q", got)
	}
}

func TestTheBottomBarLeavesOutWhatIsOff(t *testing.T) {
	got := RenderBottom("Session 1", false, false,
		"openrouter.ai", "stealth/space-bunny-alpha", "3", "ask")
	for _, unwanted := range []string{"[Mouse]", "[Copy]"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("bottom bar carries %q while off: %q", unwanted, got)
		}
	}
}

func TestTheTopBarCarriesItsFieldsInOrder(t *testing.T) {
	got := RenderTop("working", "3", "18,400", "2,104", "916", "$0.0181", "$4.82")
	wants := []string{"Status: working", "Reasoning: 3", "Context: 18,400", "In: 2,104", "Out: 916", "Cost: $0.0181", "Credits: $4.82"}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("top bar does not carry %q: %q", want, got)
		}
	}
	if head, _, ok := strings.Cut(got, " | "); ok && head != wants[0] {
		t.Errorf("top bar begins with %q, want Status", head)
	}
}

func TestBarsKeepEveryFieldAtEveryWidth(t *testing.T) {
	fields := []Field{{Name: "Credits", Value: "10"}, {Name: "Cost", Value: "1"}, {Name: "Context", Value: "20%"}}
	want := "Credits: 10 | Cost: 1 | Context: 20%"
	for _, width := range []int{0, 1, 12, 20, 24, 25, 40, 200} {
		if got := RenderBar(fields, width); got != want {
			t.Errorf("at width %d the bar is %q, want %q", width, got, want)
		}
	}
}

func TestEmptyBarFieldsAndStatusNotesStayReadable(t *testing.T) {
	bar := RenderBottom("Session 1", false, false, "", "", "", "")
	for _, want := range []string{"Provider: -", "Model: -", "Approval: -", "Verbosity: -"} {
		if !strings.Contains(bar, want) {
			t.Errorf("bottom bar does not carry %q: %q", want, bar)
		}
	}
	note := RenderTop("paused (buffered 12 rows)", "", "", "", "", "", "")
	if !strings.Contains(note, "Status: paused (buffered 12 rows)") || strings.Contains(note, "Note:") {
		t.Errorf("status note was rendered as a separate field: %q", note)
	}
}

func TestBarWithNoWidthIsNotCut(t *testing.T) {
	got := RenderBar([]Field{{Name: "Credits", Value: "10"}, {Name: "Host", Value: "localhost"}}, 0)
	if !strings.Contains(got, "Host") {
		t.Errorf("a bar with no width lost a field: %q", got)
	}
}

func stripSequences(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b {
			i += escapeLength(runes[i+1:])
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}
