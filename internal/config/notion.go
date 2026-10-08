package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const notionKey = "notion"

// Notion keeps its original bytes so a future member survives a configuration read.
type Notion struct{ raw json.RawMessage }

func (n *Notion) UnmarshalJSON(data []byte) error {
	n.raw = append(json.RawMessage(nil), data...)
	return nil
}

func (n Notion) MarshalJSON() ([]byte, error) {
	if len(n.raw) == 0 {
		return []byte("null"), nil
	}
	return n.raw, nil
}

// NotionToken returns the integration token. The credential is read only from the
// configuration file, never from the process environment.
func (c Config) NotionToken() (string, error) {
	if c.Notion == nil || isNull(c.Notion.raw) {
		return "", nil
	}
	if !isObject(c.Notion.raw) {
		return "", fmt.Errorf("config: %s: %w", notionKey, ErrNotAnObject)
	}
	var block map[string]json.RawMessage
	if err := json.Unmarshal(c.Notion.raw, &block); err != nil {
		return "", fmt.Errorf("config: %s: %w", notionKey, err)
	}
	member, ok := block["api_key"]
	if !ok || isNull(member) {
		return "", nil
	}
	var token string
	if err := json.Unmarshal(member, &token); err != nil {
		return "", fmt.Errorf("config: %s: api_key: %w", notionKey, err)
	}
	return token, nil
}

// NotionTaskSettings bounds task operations to one configured data source and
// one configured assignee property and user.
func (c Config) NotionTaskSettings() (dataSourceID, assigneeProperty, assigneeID, statusProperty string, err error) {
	if c.Notion == nil || isNull(c.Notion.raw) {
		return "", "", "", "", nil
	}
	if !isObject(c.Notion.raw) {
		return "", "", "", "", fmt.Errorf("config: %s: %w", notionKey, ErrNotAnObject)
	}
	var block map[string]json.RawMessage
	if err = json.Unmarshal(c.Notion.raw, &block); err != nil {
		return "", "", "", "", fmt.Errorf("config: %s: %w", notionKey, err)
	}
	for name, dest := range map[string]*string{
		"task_data_source_id":    &dataSourceID,
		"task_assignee_property": &assigneeProperty,
		"task_assignee_id":       &assigneeID,
		"task_status_property":   &statusProperty,
	} {
		if raw, ok := block[name]; ok && !isNull(raw) {
			if err = json.Unmarshal(raw, dest); err != nil {
				return "", "", "", "", fmt.Errorf("config: %s: %s must be a string", notionKey, name)
			}
		}
	}
	if dataSourceID == "" && assigneeProperty == "" && assigneeID == "" && statusProperty == "" {
		return "", "", "", "", nil
	}
	if !validNotionID(dataSourceID) || !validNotionID(assigneeID) || strings.TrimSpace(assigneeProperty) == "" || strings.TrimSpace(statusProperty) == "" {
		return "", "", "", "", fmt.Errorf("config: %s task access requires task_data_source_id, task_assignee_property, task_assignee_id, and task_status_property", notionKey)
	}
	if strings.TrimSpace(assigneeProperty) != assigneeProperty || strings.TrimSpace(statusProperty) != statusProperty {
		return "", "", "", "", fmt.Errorf("config: %s task property names must not have surrounding whitespace", notionKey)
	}
	return dataSourceID, assigneeProperty, assigneeID, statusProperty, nil
}

func validNotionID(value string) bool {
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return false
	}
	for _, r := range compact {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func readNotion(raw json.RawMessage) (*Notion, error) {
	if isNull(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return &Notion{raw: append(json.RawMessage(nil), raw...)}, nil
}
