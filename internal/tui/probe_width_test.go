package tui

import (
	"fmt"
	"testing"
)

// TestProbeWidth prints what each bar renders at several widths, so the behaviour of the
// cut is read off the code rather than argued about.
func TestProbeWidth(t *testing.T) {
	for _, w := range []int{1, 12, 20, 25, 40, 200} {
		top := RenderBar([]Field{
			{Name: "Status", Value: "working"},
			{Name: "Reasoning", Value: "3"},
			{Name: "Context", Value: "18,400"},
			{Name: "In", Value: "2,104"},
			{Name: "Out", Value: "916"},
			{Name: "Cost", Value: "$0.0181"},
			{Name: "Credits", Value: "$4.82"},
		}, w)
		fmt.Printf("width %3d top=%q\n", w, top)
	}

	fmt.Println()

	for _, w := range []int{1, 20, 25, 40, 200} {
		parts := []string{
			"Session 1", "[Mouse]", "Provider: openrouter.ai",
			"Model: stealth/space-bunny-alpha", "Approval: ask", "Verbosity: 3",
		}
		fmt.Printf("width %3d bottom=%q\n", w, joinWithin(parts, " | ", w))
	}
}
