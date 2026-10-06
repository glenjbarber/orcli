package tui

import (
	"strings"
	"testing"
)

func TestARealFrameIsWhatAReaderSees(t *testing.T) {
	s := New(Options{
		APIKey:     "sk-or-v1-a-key-the-frame-must-never-show",
		Model:      "stealth/space-bunny-alpha",
		Provider:   "openrouter.ai",
		Approval:   ApprovalAsk,
		WorkingDir: "/Users/gjb/Documents/github/orcli",
		Color:      true,
		Bell:       true,
	})
	s.Notice("diagnostic log row", 0, RoleNotice)

	loop := &interfaceLoop{session: s}
	state, detail := s.State()
	var status Status
	status[fieldState] = string(state) + detailSuffix(detail)
	status[fieldCwd] = loop.workingDir()
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

	screen := drawSimulationFrame(t, 20, 80, Bar{Status: status}, s.Log().Rows(), NewPalette(true, GroundDark, nil))
	text := simulationText(screen)
	if strings.Contains(text, "sk-or-v1") {
		t.Error("the credential reached a frame cell")
	}
	if !strings.Contains(text, "diagnostic log row") {
		t.Error("the session row was absent from the frame cells")
	}
	if !strings.Contains(text, "openrouter.ai") || !strings.Contains(text, "stealth/space-bunny-alpha") {
		t.Errorf("the provider or model was absent from the frame cells:\n%s", text)
	}
	t.Logf("a real frame on an 80 by 20 simulation screen:\n%s", text)
}
