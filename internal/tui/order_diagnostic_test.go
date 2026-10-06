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

	if got := strings.TrimSpace(rows[16][nameWidth()+2:]); got != "the oldest log row" {
		t.Errorf("row 17 carries %q, want oldest log row", got)
	}
	if got := strings.TrimSpace(rows[17][nameWidth()+2:]); got != "a newer log row" {
		t.Errorf("row 18 carries %q, want middle log row", got)
	}
	if got := strings.TrimSpace(rows[18][nameWidth()+2:]); got != "the newest log row" {
		t.Errorf("row 19 carries %q, want newest log row", got)
	}
	if !strings.HasPrefix(rows[19], Prompt+"THE FIELD") {
		t.Errorf("last row %q does not carry the prompt", rows[19])
	}
	t.Logf("the SimulationScreen carries %d rows from top to bottom", len(rows))
}
