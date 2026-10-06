package tui

import (
	"strings"
	"testing"
)

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
