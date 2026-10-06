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

	r, _, _, _ := screen.GetContent(0, 0)
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

	frame.SetSweepStep(10)
	frame.Draw(screen)
	_, _, cycled, _ := screen.GetContent(start, barOneRow)
	cycledFG, _, _ := cycled.Decompose()
	if cycledFG != firstFG {
		t.Fatalf("figure hue after ten steps = %v, want cycle back to %v", cycledFG, firstFG)
	}
}
