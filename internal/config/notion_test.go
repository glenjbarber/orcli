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

func TestNotionTaskSettingsBindOneSourceAndAssignee(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","notion":{"api_key":"ntn_secret","task_data_source_id":"12345678-1234-1234-1234-123456789abc","task_assignee_property":"Assignee","task_assignee_id":"abcdefab-cdef-abcd-efab-cdefabcdefab","task_status_property":"Status","future":"keep"}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	source, property, assignee, status, err := cfg.NotionTaskSettings()
	if err != nil {
		t.Fatal(err)
	}
	if source != "12345678-1234-1234-1234-123456789abc" || property != "Assignee" || assignee != "abcdefab-cdef-abcd-efab-cdefabcdefab" || status != "Status" {
		t.Fatalf("NotionTaskSettings = (%q, %q, %q, %q)", source, property, assignee, status)
	}
}

func TestNotionTaskSettingsRejectPartialOrInvalidConfiguration(t *testing.T) {
	for _, settings := range []string{
		`{"task_data_source_id":"12345678-1234-1234-1234-123456789abc"}`,
		`{"task_data_source_id":"../../pages","task_assignee_property":"Assignee","task_assignee_id":"abcdefab-cdef-abcd-efab-cdefabcdefab","task_status_property":"Status"}`,
	} {
		t.Run(settings, func(t *testing.T) {
			h := home(t)
			writeConfigAt(t, h, `{"api_key":"openrouter","notion":`+settings+`}`)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, _, err := cfg.NotionTaskSettings(); err == nil {
				t.Fatal("NotionTaskSettings accepted partial or invalid task scope")
			}
		})
	}
}
