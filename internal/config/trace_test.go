package config

import (
	"strings"
	"testing"
)

func TestWriteTracePreservesOtherConfigurationBytes(t *testing.T) {
	path := writeConfigAt(t, home(t), "{\n  \"api_key\": \"k\",\n  \"future\": {\"kept\": true}\n}\n")

	if err := WriteTrace(path, true); err != nil {
		t.Fatalf("WriteTrace on: %v", err)
	}
	data := string(readConfig(t, path))
	if !strings.Contains(data, `"api_key": "k"`) || !strings.Contains(data, `"future": {"kept": true}`) || !strings.Contains(data, `"trace": true`) {
		t.Fatalf("trace write did not preserve other members:\n%s", data)
	}

	if err := WriteTrace(path, false); err != nil {
		t.Fatalf("WriteTrace off: %v", err)
	}
	data = string(readConfig(t, path))
	if !strings.Contains(data, `"trace": false`) {
		t.Fatalf("disabled trace setting was not persisted:\n%s", data)
	}
}

func TestLoadFromPathUsesOnlyTheSelectedFile(t *testing.T) {
	selected := writeConfigAt(t, t.TempDir(), `{"api_key":"selected","trace":true}`)
	cfg, gotPath, err := LoadFromPath(selected)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if gotPath != selected || cfg.APIKey != "selected" || !cfg.Trace {
		t.Fatalf("LoadFromPath = (%+v, %q), want selected path and trace enabled", cfg, gotPath)
	}
}
