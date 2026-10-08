package config

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const apiaryKey = "apiary"

// Apiary preserves its original bytes so unknown settings survive a config round trip.
type Apiary struct{ raw json.RawMessage }

func (a *Apiary) UnmarshalJSON(data []byte) error {
	a.raw = append(json.RawMessage(nil), data...)
	return nil
}
func (a Apiary) MarshalJSON() ([]byte, error) {
	if len(a.raw) == 0 {
		return []byte("null"), nil
	}
	return a.raw, nil
}

// ApiarySettings returns the configured REST endpoint and existing Viewer token.
func (c Config) ApiarySettings() (baseURL, viewerToken string, err error) {
	if c.Apiary == nil || isNull(c.Apiary.raw) {
		return "", "", nil
	}
	if !isObject(c.Apiary.raw) {
		return "", "", fmt.Errorf("config: %s: %w", apiaryKey, ErrNotAnObject)
	}
	var block map[string]json.RawMessage
	if err = json.Unmarshal(c.Apiary.raw, &block); err != nil {
		return "", "", fmt.Errorf("config: %s: %w", apiaryKey, err)
	}
	for name, dest := range map[string]*string{"base_url": &baseURL, "viewer_token": &viewerToken} {
		if member, ok := block[name]; ok && !isNull(member) {
			if e := json.Unmarshal(member, dest); e != nil {
				return "", "", fmt.Errorf("config: %s: %s must be a string", apiaryKey, name)
			}
		}
	}
	return baseURL, viewerToken, nil
}

func readApiary(raw json.RawMessage) (*Apiary, error) {
	if isNull(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return &Apiary{raw: append(json.RawMessage(nil), raw...)}, nil
}
