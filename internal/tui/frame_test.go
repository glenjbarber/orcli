package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func drawSimulationFrame(t *testing.T, height, width int, bar Bar, rows []Row, palette Palette) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(width, height)
	frame := NewFrame()
	frame.SetRect(0, 0, width, height)
	frame.SetContent(bar, rows, palette)
	frame.Draw(screen)
	return screen
}

func simulationText(screen tcell.SimulationScreen) string {
	_, width, height := screen.GetContents()
	var b strings.Builder
	for y := range height {
		for x := 0; x < width; x++ {
			r, _, _, cellWidth := screen.GetContent(x, y)
			if cellWidth > 0 {
				b.WriteRune(r)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestFrameDrawsCellsAndPlacesPromptCaret uses a terminal exactly barRows tall, so there
// is no scrollback and the prompt is row 0.
func TestFrameDrawsCellsAndPlacesPromptCaret(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(40, barRows)

	frame := NewFrame()
	frame.SetRect(0, 0, 40, barRows)
	frame.SetContent(Bar{
		Status: Status{fieldState: "ready"},
		Field:  "hello",
	}, []Row{{Text: "reply"}}, Palette{})
	frame.SetCaret(2)
	frame.Draw(screen)

	// Prompt now has a leading space (see stack.go's Prompt doc comment), so column 0
	// is that space and column 1 is "r" of "root@lolhost".
	r, _, _, _ := screen.GetContent(1, 0)
	if r != 'r' {
		t.Fatalf("prompt row begins with %q, want 'r'", r)
	}
	x, y, visible := screen.GetCursor()
	wantX := DisplayWidth(Prompt + "he")
	if !visible || x != wantX || y != 0 {
		t.Fatalf("cursor = (%d, %d, %t), want (%d, 0, true)", x, y, visible, wantX)
	}
}

// TestFrameAppliesRowRoleStyle uses one row of scrollback above the four fixed rows, so
// the single log row lands at the top of the screen, full width, with no field column.
func TestFrameAppliesRowRoleStyle(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(60, barRows+1)

	p := NewPalette(true, GroundDark, nil)
	frame := NewFrame()
	frame.SetRect(0, 0, 60, barRows+1)
	frame.SetContent(Bar{}, []Row{{Text: "bad", Spans: []Span{{Start: 0, End: 3, Role: RoleFailure}}}}, p)
	frame.Draw(screen)

	r, _, gotStyle, _ := screen.GetContent(0, 0)
	wantFG, _, _ := p.Style(RoleFailure).Decompose()
	gotFG, _, _ := gotStyle.Decompose()
	if r != 'b' || gotFG != wantFG {
		t.Fatalf("log cell = %q with foreground %v, want %q with foreground %v", r, gotFG, 'b', wantFG)
	}
}

func TestFrameSweepsTheFigureAsCellStyles(t *testing.T) {
	bar := Bar{Status: Status{fieldFigure: "working"}}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(60, 20)
	frame := NewFrame()
	frame.SetRect(0, 0, 60, 20)
	frame.SetContent(bar, nil, plainPalette())
	frame.SetSweepStep(0)
	frame.Draw(screen)

	barOneRow := scrollbackRows(20) + 2
	prefix, _, _ := renderBarOne(bar.Status)
	start := DisplayWidth(prefix)
	_, _, first, _ := screen.GetContent(start, barOneRow)
	_, _, next, _ := screen.GetContent(start+1, barOneRow)
	firstFG, _, _ := first.Decompose()
	nextFG, _, _ := next.Decompose()
	if firstFG == nextFG {
		t.Fatal("adjacent figure cells use the same hue")
	}

	cycle := len(sweepColors(GroundDark))
	frame.SetSweepStep(cycle)
	frame.Draw(screen)
	_, _, cycled, _ := screen.GetContent(start, barOneRow)
	cycledFG, _, _ := cycled.Decompose()
	if cycledFG != firstFG {
		t.Fatalf("figure hue after %d steps = %v, want cycle back to %v", cycle, cycledFG, firstFG)
	}
}

// TestFrameDrawsAnIdleFigureInChromeNotSwept covers the bar with nothing
// running: the figure reads "idle" with no turn behind it to animate, and
// should read as plain chrome text like the rest of the bar, not a leftover
// swept colour with no figure left to explain it.
func TestFrameDrawsAnIdleFigureInChromeNotSwept(t *testing.T) {
	bar := Bar{Status: Status{fieldFigure: "idle"}}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(60, 20)
	p := plainPalette()
	frame := NewFrame()
	frame.SetRect(0, 0, 60, 20)
	frame.SetContent(bar, nil, p)
	frame.SetSweepStep(3)
	frame.Draw(screen)

	barOneRow := scrollbackRows(20) + 2
	prefix, _, _ := renderBarOne(bar.Status)
	start := DisplayWidth(prefix)

	_, _, prefixCell, _ := screen.GetContent(0, barOneRow)
	_, _, first, _ := screen.GetContent(start, barOneRow)
	_, _, next, _ := screen.GetContent(start+1, barOneRow)
	prefixFG, _, _ := prefixCell.Decompose()
	firstFG, _, _ := first.Decompose()
	nextFG, _, _ := next.Decompose()

	if firstFG != prefixFG {
		t.Errorf("idle figure foreground = %v, want the bar's own chrome %v", firstFG, prefixFG)
	}
	if firstFG != nextFG {
		t.Error("adjacent idle figure cells use different hues, want the same flat chrome")
	}
}

// screenRowText reads width cells back from the screen starting at (x, y) and
// trims the trailing spaces the base fill leaves behind, so a test can compare
// what a row drew against a plain string.
func screenRowText(screen tcell.SimulationScreen, x, y, width int) string {
	var b strings.Builder
	for col := 0; col < width; col++ {
		r, _, _, _ := screen.GetContent(x+col, y)
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " ")
}

// TestFrameFoldsAMultiLineRowAcrossRows covers the bug a reply's own line
// breaks used to hit: a row carrying "\n" is no longer collapsed onto one
// screen line and cut with an ellipsis, it is folded into one physical line
// per line break, each shown in the order the model wrote them.
func TestFrameFoldsAMultiLineRowAcrossRows(t *testing.T) {
	height := barRows + 3
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(40, height)

	frame := NewFrame()
	frame.SetRect(0, 0, 40, height)
	frame.SetContent(Bar{}, []Row{{Text: "first line\nsecond line\nthird line"}}, Palette{})
	frame.Draw(screen)

	backlog := scrollbackRows(height)
	if backlog != 3 {
		t.Fatalf("scrollbackRows(%d) = %d, want 3: the test assumes one row per line", height, backlog)
	}

	want := []string{"first line", "second line", "third line"}
	for i, line := range want {
		if got := screenRowText(screen, 0, i, 40); got != line {
			t.Errorf("scrollback row %d = %q, want %q", i, got, line)
		}
	}
}

// TestFrameFoldsSpansWithTheirOwnLine covers a span that falls entirely
// within one line of a folded row: the fold rebases it rather than losing it
// or leaving it pointing at another line's text.
func TestFrameFoldsSpansWithTheirOwnLine(t *testing.T) {
	text := "plain\nBOLD\nplain"
	start := strings.Index(text, "BOLD")
	row := Row{Text: text, Spans: []Span{{Start: start, End: start + len("BOLD"), Role: RoleEmphasis}}}

	lines := foldRowLines(row)
	if len(lines) != 3 {
		t.Fatalf("foldRowLines returned %d lines, want 3", len(lines))
	}
	if len(lines[0].Spans) != 0 {
		t.Errorf("line 0 (%q) carries a span, want none", lines[0].Text)
	}
	if len(lines[2].Spans) != 0 {
		t.Errorf("line 2 (%q) carries a span, want none", lines[2].Text)
	}
	if got := lines[1].Spans; len(got) != 1 || got[0].Start != 0 || got[0].End != len("BOLD") {
		t.Errorf("line 1's span = %v, want one span covering the whole of %q", got, lines[1].Text)
	}
}

// TestFoldRowLinesLeavesASingleLineRowUnchanged covers the common case: a
// row with no line break of its own costs nothing extra and is not copied
// into a new slice just to hold the same row.
func TestFoldRowLinesLeavesASingleLineRowUnchanged(t *testing.T) {
	row := Row{Text: "no line break here", Spans: []Span{{Start: 0, End: 2, Role: RoleCode}}}
	lines := foldRowLines(row)
	if len(lines) != 1 {
		t.Fatalf("foldRowLines returned %d lines, want 1", len(lines))
	}
	if lines[0].Text != row.Text {
		t.Errorf("text = %q, want %q", lines[0].Text, row.Text)
	}
}
