package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestKeyEventMapsTCellInput(t *testing.T) {
	cases := []struct {
		name string
		code tcell.Key
		rune rune
		want Key
	}{
		{"rune", tcell.KeyRune, 'é', KeyRune},
		{"enter", tcell.KeyEnter, 0, KeyEnter},
		{"backspace", tcell.KeyBackspace, 0, KeyBackspace},
		{"backspace2", tcell.KeyBackspace2, 0, KeyBackspace},
		{"delete", tcell.KeyDelete, 0, KeyDelete},
		{"left", tcell.KeyLeft, 0, KeyLeft},
		{"right", tcell.KeyRight, 0, KeyRight},
		{"up", tcell.KeyUp, 0, KeyUp},
		{"down", tcell.KeyDown, 0, KeyDown},
		{"home", tcell.KeyHome, 0, KeyHome},
		{"end", tcell.KeyEnd, 0, KeyEnd},
		{"tab", tcell.KeyTab, 0, KeyTab},
		{"escape", tcell.KeyEscape, 0, KeyEscape},
		{"control-c", tcell.KeyCtrlC, 0, KeyCtrlC},
		{"control-d", tcell.KeyCtrlD, 0, KeyEOF},
		{"control-a", tcell.KeyCtrlA, 0, KeyCtrlA},
		{"control-w", tcell.KeyCtrlW, 0, KeyCtrlW},
		{"control-u", tcell.KeyCtrlU, 0, KeyCtrlU},
		{"unknown", tcell.KeyF1, 0, KeyNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, r := keyEvent(tcell.NewEventKey(tc.code, tc.rune, tcell.ModNone))
			if got != tc.want {
				t.Fatalf("keyEvent returned %v, want %v", got, tc.want)
			}
			if tc.want == KeyRune && r != tc.rune {
				t.Fatalf("keyEvent rune = %q, want %q", r, tc.rune)
			}
		})
	}
}
