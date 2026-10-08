package config

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func readNotion(raw json.RawMessage) (*Notion, error) {
	if isNull(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return &Notion{raw: append(json.RawMessage(nil), raw...)}, nil
}
