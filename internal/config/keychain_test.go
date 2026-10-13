package config

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeychainBlockIsReadAndEnumerable(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"inline-key","keychain":{"items":{"openrouter":{"account":"john@smith.com","service":"private-openrouter-api"},"test":{"account":"john@smith.com","service":"orcli-test-key"}}}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Keychain == nil {
		t.Fatal("Keychain is nil")
	}
	if len(cfg.Keychain.Items) != 2 {
		t.Fatalf("Items = %v, want 2 entries", cfg.Keychain.Items)
	}
	got := cfg.Keychain.Items["openrouter"]
	want := KeychainRef{Account: "john@smith.com", Service: "private-openrouter-api"}
	if got != want {
		t.Errorf("Items[openrouter] = %+v, want %+v", got, want)
	}
}

func TestResolveAPIKey_InlineAlwaysWinsOverKeychain(t *testing.T) {
	cfg := Config{
		APIKey:         "inline-key",
		APIKeyKeychain: "openrouter",
		Keychain:       &Keychain{Items: map[string]KeychainRef{"openrouter": {Account: "a", Service: "s"}}},
	}
	called := false
	lookup := func(ctx context.Context, ref KeychainRef) (string, error) {
		called = true
		return "from-keychain", nil
	}

	got, err := cfg.ResolveAPIKey(context.Background(), lookup)
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if got != "inline-key" {
		t.Errorf("ResolveAPIKey() = %q, want the inline key", got)
	}
	if called {
		t.Error("the keychain lookup ran despite an inline api_key being present")
	}
}

func TestResolveAPIKey_FallsBackToKeychainWhenNoInlineKey(t *testing.T) {
	ref := KeychainRef{Account: "john@smith.com", Service: "private-openrouter-api"}
	cfg := Config{
		APIKeyKeychain: "openrouter",
		Keychain:       &Keychain{Items: map[string]KeychainRef{"openrouter": ref}},
	}
	var gotRef KeychainRef
	lookup := func(ctx context.Context, r KeychainRef) (string, error) {
		gotRef = r
		return "resolved-secret", nil
	}

	got, err := cfg.ResolveAPIKey(context.Background(), lookup)
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if got != "resolved-secret" {
		t.Errorf("ResolveAPIKey() = %q, want the keychain-resolved secret", got)
	}
	if gotRef != ref {
		t.Errorf("lookup was called with %+v, want %+v", gotRef, ref)
	}
}

func TestResolveAPIKey_NoInlineNoKeychainReturnsErrNoAPIKey(t *testing.T) {
	cfg := Config{}
	_, err := cfg.ResolveAPIKey(context.Background(), func(context.Context, KeychainRef) (string, error) {
		t.Fatal("lookup should not be called with no api_key_keychain configured")
		return "", nil
	})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("ResolveAPIKey() = %v, want ErrNoAPIKey", err)
	}
}

func TestResolveAPIKey_NamedItemMissingFromKeychainBlock(t *testing.T) {
	cfg := Config{
		APIKeyKeychain: "openrouter",
		Keychain:       &Keychain{Items: map[string]KeychainRef{"groq": {Account: "a", Service: "s"}}},
	}
	_, err := cfg.ResolveAPIKey(context.Background(), func(context.Context, KeychainRef) (string, error) {
		t.Fatal("lookup should not be called for an unconfigured name")
		return "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "openrouter") {
		t.Errorf("ResolveAPIKey() = %v, want an error naming the missing item", err)
	}
}

func TestResolveAPIKey_NoKeychainBlockAtAll(t *testing.T) {
	cfg := Config{APIKeyKeychain: "openrouter"}
	_, err := cfg.ResolveAPIKey(context.Background(), func(context.Context, KeychainRef) (string, error) {
		t.Fatal("lookup should not be called with no keychain block")
		return "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "openrouter") {
		t.Errorf("ResolveAPIKey() = %v, want an error naming what was asked for", err)
	}
}

func TestResolveAPIKey_PropagatesALookupFailureUnwrapped(t *testing.T) {
	sentinel := errors.New("keychain: no matching item")
	cfg := Config{
		APIKeyKeychain: "openrouter",
		Keychain:       &Keychain{Items: map[string]KeychainRef{"openrouter": {Account: "a", Service: "s"}}},
	}
	_, err := cfg.ResolveAPIKey(context.Background(), func(context.Context, KeychainRef) (string, error) {
		return "", sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("ResolveAPIKey() = %v, want it to wrap/return the lookup's own error", err)
	}
}

func TestWriteKeychainItem_AddsAndUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orcli.json")
	writeConfig(t, path, `{"api_key":"k"}`, FileMode)

	if err := WriteKeychainItem(path, "openrouter", KeychainRef{Account: "john@smith.com", Service: "private-openrouter-api"}); err != nil {
		t.Fatalf("WriteKeychainItem: %v", err)
	}
	cfg, _, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if cfg.Keychain == nil || cfg.Keychain.Items["openrouter"] != (KeychainRef{Account: "john@smith.com", Service: "private-openrouter-api"}) {
		t.Fatalf("item was not written: %+v", cfg.Keychain)
	}
	// api_key must survive untouched - this writer only edits the
	// keychain member.
	if cfg.APIKey != "k" {
		t.Errorf("api_key was disturbed: %q", cfg.APIKey)
	}

	// Update in place: adding a second named item must not disturb the
	// first.
	if err := WriteKeychainItem(path, "groq", KeychainRef{Account: "john@smith.com", Service: "private-groq-api"}); err != nil {
		t.Fatalf("WriteKeychainItem (second): %v", err)
	}
	cfg, _, err = LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath (second): %v", err)
	}
	if len(cfg.Keychain.Items) != 2 {
		t.Fatalf("Items = %v, want both entries present", cfg.Keychain.Items)
	}
}

func TestWriteKeychainItem_RefusesAnIncompleteReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orcli.json")
	writeConfig(t, path, `{"api_key":"k"}`, FileMode)

	if err := WriteKeychainItem(path, "openrouter", KeychainRef{Account: "john@smith.com"}); err == nil {
		t.Error("expected a refusal for a reference with no service")
	}
}

func TestDeleteKeychainItem_RemovesOnlyTheNamedItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orcli.json")
	writeConfig(t, path, `{"api_key":"k"}`, FileMode)

	if err := WriteKeychainItem(path, "openrouter", KeychainRef{Account: "a", Service: "s1"}); err != nil {
		t.Fatalf("WriteKeychainItem: %v", err)
	}
	if err := WriteKeychainItem(path, "groq", KeychainRef{Account: "a", Service: "s2"}); err != nil {
		t.Fatalf("WriteKeychainItem: %v", err)
	}

	if err := DeleteKeychainItem(path, "openrouter"); err != nil {
		t.Fatalf("DeleteKeychainItem: %v", err)
	}
	cfg, _, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if _, ok := cfg.Keychain.Items["openrouter"]; ok {
		t.Error("deleted item is still present")
	}
	if _, ok := cfg.Keychain.Items["groq"]; !ok {
		t.Error("the other item was removed too")
	}
}

func TestDeleteKeychainItem_RefusesAnUnknownName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orcli.json")
	writeConfig(t, path, `{"api_key":"k","keychain":{"items":{"openrouter":{"account":"a","service":"s"}}}}`, FileMode)

	if err := DeleteKeychainItem(path, "groq"); err == nil {
		t.Error("expected a refusal for a name that was never configured")
	}
	// And the existing item must survive the refused call untouched.
	cfg, _, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if _, ok := cfg.Keychain.Items["openrouter"]; !ok {
		t.Error("the existing item was lost by a refused delete")
	}
}
