package config

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Cloudflare is the block holding what the Cloudflare commands need.
//
// It is a nested object rather than a set of flat top-level members, so a further
// provider can be added under the same shape without changing the file's top level.
// A file with one provider has one block beside the OpenRouter key; a file with
// three has three blocks, and the shape a reader learns once is the shape every
// provider uses.
//
// The block holds its bytes rather than a decoded struct, and that is deliberate. A
// member of it that this client does not name must survive a read and a write
// untouched: the file holds a credential, and a round trip that loses a key is a file
// the reader has to repair by hand before this client will open it again.
//
// There is no writer. The reader edits the file by hand, since a key is something a
// person puts in a file rather than something a command accepts at a prompt. A writer
// is added with the command that sets it, and every writer is named here: this file
// holds a credential and only this package rewrites it.
type Cloudflare struct {
	// raw is the block as it was written, bytes and all.
	raw json.RawMessage
}

// cloudflareKey is the member the block lives under.
//
// It is named once so a reader and a writer cannot disagree about the spelling,
// which is the same reason trustedKey is.
const cloudflareKey = "cloudflare"

// UnmarshalJSON keeps the block as bytes.
//
// It is implemented rather than left to a decode so that the raw bytes survive, which
// a decode into a struct cannot do: a member of this block that this client does not
// name would be dropped, and any writer writing the struct back would take the
// reader's key with it.
func (c *Cloudflare) UnmarshalJSON(data []byte) error {
	c.raw = append(json.RawMessage(nil), data...)
	return nil
}

// MarshalJSON writes the block back exactly as it was read.
//
// It is the same rule as UnmarshalJSON from the other end. The only writer of this
// file is one that edits bytes for the member it was asked to change, and anything
// else is a second thing that can silently reshape a credential file.
func (c Cloudflare) MarshalJSON() ([]byte, error) {
	if len(c.raw) == 0 {
		return []byte("null"), nil
	}
	return c.raw, nil
}

// APIKey returns the Cloudflare credential, and an error for a block of the wrong
// shape.
//
// The empty string is the ordinary first-run state rather than a fault: a reader who
// has never used the command has a valid configuration, and a startup that refused
// over it would lock them out of the interface over a preference they have not
// expressed.
//
// The error is for a block that is not an object, or a key that is not a string. A
// block the reader wrote by hand and got wrong is reported rather than ignored, since
// a provider silently reading as having no key is a command that tells a reader their
// credential is missing while they are looking at one.
func (c Config) CloudflareAPIKey() (string, error) {
	if c.Cloudflare == nil || isNull(c.Cloudflare.raw) {
		return "", nil
	}

	if !isObject(c.Cloudflare.raw) {
		return "", fmt.Errorf("config: %s: %w", cloudflareKey, ErrNotAnObject)
	}

	var block map[string]json.RawMessage
	if err := json.Unmarshal(c.Cloudflare.raw, &block); err != nil {
		return "", fmt.Errorf("config: %s: %w", cloudflareKey, err)
	}

	member, found := block["api_key"]
	if !found || isNull(member) {
		return "", nil
	}

	var key string
	if err := json.Unmarshal(member, &key); err != nil {
		return "", fmt.Errorf("config: %s: api_key: %w", cloudflareKey, err)
	}
	return key, nil
}

// readCloudflare reads the block, and reports nothing when it is absent or null.
//
// It is called from decode rather than left to the decoder, so the shape is judged
// where the key is read rather than at startup. A reader with a block of an
// unexpected shape is not refused at startup over a provider they never called, and
// the error they get names the thing they were doing rather than a file load.
func readCloudflare(raw json.RawMessage) (*Cloudflare, error) {
	switch {
	case isNull(raw), len(bytes.TrimSpace(raw)) == 0:
		return nil, nil
	}
	return &Cloudflare{raw: append(json.RawMessage(nil), raw...)}, nil
}
