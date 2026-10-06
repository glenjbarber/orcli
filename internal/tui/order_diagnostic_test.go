package tui

import (
	"strings"
	"testing"
)

func TestTheFrameOrderIsTopToBottom(t *testing.T) {
	log := []Row{
		{Text: "the oldest log row"},
		{Text: "a newer log row"},
		{Text: "the newest log row"},
	}
	screen := drawSimulationFrame(t, 20, 80, Bar{Field: "THE FIELD"}, log, Palette{})
	rows := strings.Split(strings.TrimRight(simulationText(screen), "\n"), "\n")
	promptRow := scrollbackRows(20)

	if got := strings.TrimSpace(rows[promptRow-3]); got != "the oldest log row" {
		t.Errorf("row %d carries %q, want oldest log row", promptRow-3, got)
	}
	if got := strings.TrimSpace(rows[promptRow-2]); got != "a newer log row" {
		t.Errorf("row %d carries %q, want middle log row", promptRow-2, got)
	}
	if got := strings.TrimSpace(rows[promptRow-1]); got != "the newest log row" {
		t.Errorf("row %d carries %q, want newest log row", promptRow-1, got)
	}
	if !strings.HasPrefix(rows[promptRow], Prompt+"THE FIELD") {
		t.Errorf("prompt row %q does not carry the prompt", rows[promptRow])
	}
	t.Logf("the SimulationScreen carries %d rows from top to bottom", len(rows))
}
