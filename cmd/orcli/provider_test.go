package main

import (
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/groq"
	"github.com/glenjbarber/orcli/internal/openrouter"
)

// TestIsGroqProvider covers the exact-match rule isGroqProvider's own doc
// comment states: only "groq", compared case-insensitively and trimmed, and
// nothing that merely contains it, selects Groq.
func TestIsGroqProvider(t *testing.T) {
	for _, tc := range []struct {
		provider string
		want     bool
	}{
		{"groq", true},
		{"Groq", true},
		{"GROQ", true},
		{"  groq  ", true},
		{"", false},
		{"openrouter.ai", false},
		{"groq.com", false},
		{"not-groq", false},
	} {
		if got := isGroqProvider(tc.provider); got != tc.want {
			t.Errorf("isGroqProvider(%q) = %v, want %v", tc.provider, got, tc.want)
		}
	}
}

// TestNewDispatcherForChoosesGroqClient covers newDispatcherFor's own
// construction of newORClient: a configuration naming Groq builds a
// *groq.Client, and every other configuration - including the default - still
// builds a *openrouter.Client, so a reader's existing configuration file,
// written before Groq support existed, keeps behaving exactly as it did.
func TestNewDispatcherForChoosesGroqClient(t *testing.T) {
	d := newDispatcherFor(config.Config{APIKey: "k", Provider: "groq"})
	client := d.newORClient("k")
	if _, ok := client.(*groq.Client); !ok {
		t.Errorf("newORClient built a %T for provider %q, want *groq.Client", client, "groq")
	}
}

// TestNewDispatcherForDefaultsToOpenRouter covers the other half of the same
// choice: a configuration with no provider named, or one naming OpenRouter's
// own host, builds a *openrouter.Client.
func TestNewDispatcherForDefaultsToOpenRouter(t *testing.T) {
	for _, provider := range []string{"", "openrouter.ai"} {
		d := newDispatcherFor(config.Config{APIKey: "k", Provider: provider})
		client := d.newORClient("k")
		if _, ok := client.(*openrouter.Client); !ok {
			t.Errorf("newORClient built a %T for provider %q, want *openrouter.Client", client, provider)
		}
	}
}

// TestNewTransportChoosesGroq covers the sibling seam in main.go: a
// configuration naming Groq builds a *groq.Client for the turn, satisfying
// chatClient the same way *openrouter.Client does.
func TestNewTransportChoosesGroq(t *testing.T) {
	client := newTransport(config.Config{APIKey: "k", Provider: "groq"})
	if _, ok := client.(*groq.Client); !ok {
		t.Errorf("newTransport built a %T for provider %q, want *groq.Client", client, "groq")
	}
}

// TestNewTransportDefaultsToOpenRouter mirrors
// TestNewDispatcherForDefaultsToOpenRouter for newTransport.
func TestNewTransportDefaultsToOpenRouter(t *testing.T) {
	for _, provider := range []string{"", "openrouter.ai"} {
		client := newTransport(config.Config{APIKey: "k", Provider: provider})
		if _, ok := client.(*openrouter.Client); !ok {
			t.Errorf("newTransport built a %T for provider %q, want *openrouter.Client", client, provider)
		}
	}
}
