package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/keychain"
)

// stubKeychain stands in for internal/keychain's own Set/Delete for the
// duration of one test, restoring the real functions afterward - the same
// pattern runIn's own doc comment describes for the trust gate.
func stubKeychain(t *testing.T, set func(context.Context, keychain.Ref, string) error, del func(context.Context, keychain.Ref) error) {
	t.Helper()
	oldSet, oldDelete := keychainSet, keychainDelete
	if set != nil {
		keychainSet = set
	}
	if del != nil {
		keychainDelete = del
	}
	t.Cleanup(func() {
		keychainSet, keychainDelete = oldSet, oldDelete
	})
}

func TestKeychainSet_WritesTheConfigReferenceAndTheSecret(t *testing.T) {
	path := writeConfigPath(t, `{"api_key":"k"}`)
	var gotRef keychain.Ref
	var gotSecret string
	stubKeychain(t, func(ctx context.Context, ref keychain.Ref, secret string) error {
		gotRef, gotSecret = ref, secret
		return nil
	}, nil)

	var out bytes.Buffer
	err := runKeychainCommand(context.Background(), []string{"set", "openrouter", "john@smith.com", "private-openrouter-api"},
		strings.NewReader("sk-or-v1-secret\n"), &out, config.Config{}, path)
	if err != nil {
		t.Fatalf("runKeychainCommand: %v", err)
	}
	if gotSecret != "sk-or-v1-secret" {
		t.Errorf("Set was called with secret %q", gotSecret)
	}
	if gotRef != (keychain.Ref{Account: "john@smith.com", Service: "private-openrouter-api"}) {
		t.Errorf("Set was called with %+v", gotRef)
	}
	if strings.Contains(out.String(), "sk-or-v1-secret") {
		t.Errorf("the secret was printed to stdout: %q", out.String())
	}

	cfg, _, err := config.LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if cfg.Keychain == nil || cfg.Keychain.Items["openrouter"] != (config.KeychainRef{Account: "john@smith.com", Service: "private-openrouter-api"}) {
		t.Errorf("the config reference was not written: %+v", cfg.Keychain)
	}
}

func TestKeychainSet_WrongArgCount(t *testing.T) {
	path := writeConfigPath(t, `{"api_key":"k"}`)
	var out bytes.Buffer
	err := runKeychainCommand(context.Background(), []string{"set", "openrouter", "only-account"},
		strings.NewReader(""), &out, config.Config{}, path)
	if err == nil {
		t.Fatal("expected an error for the wrong argument count")
	}
}

func TestKeychainSet_EmptySecretIsRefused(t *testing.T) {
	path := writeConfigPath(t, `{"api_key":"k"}`)
	stubKeychain(t, func(context.Context, keychain.Ref, string) error {
		t.Fatal("Set should not be called when no secret was given")
		return nil
	}, nil)

	var out bytes.Buffer
	err := runKeychainCommand(context.Background(), []string{"set", "openrouter", "a", "s"},
		strings.NewReader("\n"), &out, config.Config{}, path)
	if err == nil {
		t.Fatal("expected a refusal for an empty secret")
	}
}

func TestKeychainSet_KeychainFailureLeavesNoConfigReference(t *testing.T) {
	path := writeConfigPath(t, `{"api_key":"k"}`)
	sentinel := errors.New("keychain: locked")
	stubKeychain(t, func(context.Context, keychain.Ref, string) error {
		return sentinel
	}, nil)

	var out bytes.Buffer
	err := runKeychainCommand(context.Background(), []string{"set", "openrouter", "a", "s"},
		strings.NewReader("secret\n"), &out, config.Config{}, path)
	if !errors.Is(err, sentinel) {
		t.Fatalf("runKeychainCommand() = %v, want it to propagate the keychain failure", err)
	}
	cfg, _, err := config.LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if cfg.Keychain != nil && len(cfg.Keychain.Items) != 0 {
		t.Errorf("a config reference was written despite the Keychain write failing: %+v", cfg.Keychain)
	}
}

func TestKeychainList_NeverPrintsASecretAndMarksTheActiveOne(t *testing.T) {
	cfg := config.Config{
		APIKeyKeychain: "openrouter",
		Keychain: &config.Keychain{Items: map[string]config.KeychainRef{
			"openrouter": {Account: "john@smith.com", Service: "private-openrouter-api"},
			"test":       {Account: "john@smith.com", Service: "orcli-test-key"},
		}},
	}
	var out bytes.Buffer
	if err := runKeychainCommand(context.Background(), []string{"list"}, strings.NewReader(""), &out, cfg, "/unused"); err != nil {
		t.Fatalf("runKeychainCommand: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "openrouter") || !strings.Contains(s, "test") {
		t.Errorf("list did not mention both items:\n%s", s)
	}
	if !strings.Contains(s, "(api_key_keychain)") {
		t.Errorf("list did not mark which item is active:\n%s", s)
	}
	// The account value itself ("john@smith.com") is not a secret - it is
	// the reference the file already carries in the open - so only the
	// service names stand in here for what must never appear: a secret
	// value never passes through this path at all, by construction, since
	// runKeychainList never touches internal/keychain.
}

func TestKeychainList_NoneConfigured(t *testing.T) {
	var out bytes.Buffer
	if err := runKeychainCommand(context.Background(), []string{"list"}, strings.NewReader(""), &out, config.Config{}, "/unused"); err != nil {
		t.Fatalf("runKeychainCommand: %v", err)
	}
	if !strings.Contains(out.String(), "no Keychain items") {
		t.Errorf("list output = %q", out.String())
	}
}

func TestKeychainDelete_RemovesTheConfigReferenceAndTheKeychainItem(t *testing.T) {
	path := writeConfigPath(t, `{"api_key":"k","keychain":{"items":{"openrouter":{"account":"a","service":"s"}}}}`)
	var gotRef keychain.Ref
	stubKeychain(t, nil, func(ctx context.Context, ref keychain.Ref) error {
		gotRef = ref
		return nil
	})
	cfg, _, err := config.LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}

	var out bytes.Buffer
	if err := runKeychainCommand(context.Background(), []string{"delete", "openrouter"}, strings.NewReader(""), &out, cfg, path); err != nil {
		t.Fatalf("runKeychainCommand: %v", err)
	}
	if gotRef != (keychain.Ref{Account: "a", Service: "s"}) {
		t.Errorf("Delete was called with %+v", gotRef)
	}
	after, _, err := config.LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath (after): %v", err)
	}
	if after.Keychain != nil {
		if _, ok := after.Keychain.Items["openrouter"]; ok {
			t.Error("the config reference was not removed")
		}
	}
}

func TestKeychainDelete_UnknownNameIsRefused(t *testing.T) {
	path := writeConfigPath(t, `{"api_key":"k"}`)
	stubKeychain(t, nil, func(context.Context, keychain.Ref) error {
		t.Fatal("Delete should not be called for a name that was never configured")
		return nil
	})

	var out bytes.Buffer
	err := runKeychainCommand(context.Background(), []string{"delete", "openrouter"}, strings.NewReader(""), &out, config.Config{}, path)
	if err == nil {
		t.Fatal("expected a refusal for an unconfigured name")
	}
}

func TestKeychainCommand_UnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	err := runKeychainCommand(context.Background(), []string{"frobnicate"}, strings.NewReader(""), &out, config.Config{}, "/unused")
	if err == nil {
		t.Fatal("expected a refusal for an unknown subcommand")
	}
}

// writeConfigPath writes a configuration file at a fresh temporary path
// (not the shared default-search-order path withHome/writeConfig use), so
// tests here can run without withHome's own HOME/SearchOrder stand-in,
// mirroring how the rest of this package's tests pass an explicit path to
// config.LoadFromPath directly.
func writeConfigPath(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
