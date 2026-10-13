package config

import (
	"context"
	"encoding/json"
	"fmt"
)

// keychainKey is the member the Keychain block lives under, named once so a
// reader and a writer cannot disagree about the spelling.
const keychainKey = "keychain"

// KeychainRef is one named Keychain item's account/service pair - the
// reference a configuration file carries instead of a credential (issue
// #67). It mirrors internal/keychain's own Ref, as a plain struct rather
// than a shared type: this package has no dependency on internal/keychain
// today (ResolveAPIKey takes a lookup function instead, see its own doc
// comment for why), and a reference that lives in a file is a different
// thing from a reference a lookup takes, even though the two are shaped the
// same.
type KeychainRef struct {
	Account string `json:"account"`
	Service string `json:"service"`
}

// Keychain is the block naming every Keychain item this file knows about.
//
// Unlike Cloudflare/Notion/GitHub/Apiary above, this block is NOT kept as
// raw bytes. Those three are read-only from this package's own side - "the
// reader edits the file by hand" - and keeping their bytes verbatim is what
// lets a future member survive a round trip with no writer here to lose it.
// This block needs a writer (WriteKeychainItem, for the add/update flow
// issue #67 asks for), and unlike api_key or a model identifier, nothing
// under this key is ever a secret: an account and a service name are a
// reference, not a credential, so decoding and re-rendering this one block
// carries none of the risk a full-file re-encode would for the rest of the
// file (see WriteModel's own doc comment for that risk).
type Keychain struct {
	// Items maps a name (what api_key_keychain, or a provider block's own
	// future equivalent, points at) to the account/service pair that
	// resolves it.
	Items map[string]KeychainRef `json:"items,omitempty"`
}

// readKeychain decodes the keychain block, used by Config.decode.
func readKeychain(raw json.RawMessage) (*Keychain, error) {
	if isNull(raw) {
		return nil, nil
	}
	var k Keychain
	if err := json.Unmarshal(raw, &k); err != nil {
		return nil, fmt.Errorf("%s: %w", keychainKey, err)
	}
	return &k, nil
}

// WriteKeychainItem adds or updates one named Keychain reference.
//
// It never touches a secret value - only the account/service reference - so
// unlike WriteModel it has no key-material byte-copying concern of its own.
// It still edits only the "keychain" top-level member and leaves the rest
// of the file untouched, on the same grounds every writer here does: a
// reader's own formatting elsewhere in the file is not this package's to
// reflow.
func WriteKeychainItem(path, name string, ref KeychainRef) error {
	if name == "" {
		return fmt.Errorf("config: no keychain item name was given")
	}
	if ref.Account == "" || ref.Service == "" {
		return fmt.Errorf("config: keychain item %q needs both an account and a service", name)
	}

	data, err := readForEdit(path)
	if err != nil {
		return err
	}

	k, err := currentKeychain(data)
	if err != nil {
		return err
	}
	if k.Items == nil {
		k.Items = map[string]KeychainRef{}
	}
	k.Items[name] = ref

	return writeKeychainBlock(path, data, k)
}

// DeleteKeychainItem removes one named reference from the file. It is the
// config-side half of issue #67's "deletion should remove only the
// specifically selected item" requirement - the Keychain-side half is
// internal/keychain.Delete, a separate call a caller makes alongside this
// one, since removing the reference and removing the secret are two
// different systems and a caller may want either without the other (for
// example, retiring a reference to an item kept for another tool).
func DeleteKeychainItem(path, name string) error {
	if name == "" {
		return fmt.Errorf("config: no keychain item name was given")
	}

	data, err := readForEdit(path)
	if err != nil {
		return err
	}
	k, err := currentKeychain(data)
	if err != nil {
		return err
	}
	if _, ok := k.Items[name]; !ok {
		return fmt.Errorf("config: keychain item %q is not configured", name)
	}
	delete(k.Items, name)

	return writeKeychainBlock(path, data, k)
}

// currentKeychain reads the keychain block already in data, or an empty one
// if the file has none yet.
func currentKeychain(data []byte) (Keychain, error) {
	span, found := findMember(data, keychainKey)
	if !found {
		return Keychain{}, nil
	}
	var k Keychain
	if err := json.Unmarshal(data[span.start:span.end], &k); err != nil {
		return Keychain{}, fmt.Errorf("config: the existing %s member does not parse: %w", keychainKey, err)
	}
	return k, nil
}

// writeKeychainBlock renders k and sets it as data's own "keychain" member.
func writeKeychainBlock(path string, data []byte, k Keychain) error {
	rendered, err := json.Marshal(k)
	if err != nil {
		return fmt.Errorf("config: rendering the keychain block: %w", err)
	}
	edited, err := setMember(data, keychainKey, string(rendered))
	if err != nil {
		return err
	}
	if err := checkMember(edited, keychainKey, string(rendered)); err != nil {
		return fmt.Errorf("config: the edit did not verify: %w", err)
	}
	return osWriteFile(path, edited)
}

// KeychainLookupFunc resolves one Keychain reference to a secret.
//
// ResolveAPIKey takes this as a parameter rather than importing
// internal/keychain directly, because this package is read by every command
// (including ones that never touch a credential, like /level) and has never
// depended on anything that shells out to an external program. A caller in
// cmd/orcli passes keychain.Lookup, adapted to this signature, at the one
// call site that actually needs a live Keychain - everywhere else in this
// package keeps testing configuration parsing without a subprocess.
type KeychainLookupFunc func(ctx context.Context, ref KeychainRef) (string, error)

// ResolveAPIKey returns the credential a session should start with.
//
// Precedence is deliberate and is issue #67's own open question, settled
// here: an inline api_key, when present, always wins over api_key_keychain.
// This is the least surprising choice for a reader migrating one field at a
// time - adding api_key_keychain to a file that still carries a stale
// api_key must not silently stop using the value the reader can still see
// in the file, since a security-relevant field going quiet with no error is
// exactly the failure mode this whole feature exists to avoid elsewhere.
// Migrating means removing api_key from the file, not just adding
// api_key_keychain beside it.
//
// An empty api_key with no api_key_keychain returns ErrNoAPIKey unchanged,
// which is the same sentinel Load already returns for that case - this
// function is meant to be called after Load's own ErrNoAPIKey, as a second
// attempt, not as a replacement for the check Load already makes.
func (c Config) ResolveAPIKey(ctx context.Context, lookup KeychainLookupFunc) (string, error) {
	if c.APIKey != "" {
		return c.APIKey, nil
	}
	if c.APIKeyKeychain == "" {
		return "", ErrNoAPIKey
	}
	if c.Keychain == nil {
		return "", fmt.Errorf("config: api_key_keychain names %q, but this file has no keychain.items block", c.APIKeyKeychain)
	}
	ref, ok := c.Keychain.Items[c.APIKeyKeychain]
	if !ok {
		return "", fmt.Errorf("config: api_key_keychain names %q, which is not in keychain.items", c.APIKeyKeychain)
	}
	return lookup(ctx, ref)
}
