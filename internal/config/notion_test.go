package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNotionTokenIsReadAndUnknownMembersSurvive(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","notion":{"api_key":"ntn_exampletoken123456789","future":"keep"}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := cfg.NotionToken()
	if err != nil {
		t.Fatalf("NotionToken: %v", err)
	}
	if got != "ntn_exampletoken123456789" {
		t.Fatalf("NotionToken = %q", got)
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"future":"keep"`) {
		t.Errorf("unknown Notion member was lost: %s", raw)
	}
}

func TestNotionTokenRejectsWrongShape(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","notion":{"api_key":42}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.NotionToken(); err == nil {
		t.Fatal("NotionToken accepted a non-string credential")
	}
}
