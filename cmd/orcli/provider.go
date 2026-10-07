package main

import "strings"

// groqProviderName is the one string this program recognizes as selecting
// Groq rather than OpenRouter.
//
// It is compared case-insensitively against the trimmed configuration value,
// not matched as a substring, since Provider is a reader-written string
// field with no enum behind it (see config.Config.Provider's own doc
// comment) and a substring match would make a provider string that merely
// mentions "groq" in passing - a comment a reader left themselves, or a
// future provider whose own name happens to contain it - silently choose a
// transport the reader never asked for.
//
// This name is not a model identifier, so it carries none of the staleness
// risk the Groq task record warns against: Groq can add or remove models
// from its free tier at any time, but the provider's own name is not one of
// them, and nothing in this program ever reads a model id except from a live
// /models response (see internal/groq.Client.Models).
const groqProviderName = "groq"

// isGroqProvider reports whether a configured provider string selects Groq
// rather than OpenRouter.
//
// Every value other than "groq" - including the default, "openrouter.ai",
// and the empty string a session with no provider configured carries -
// selects OpenRouter. That default is deliberate: OpenRouter was this
// program's only provider before Groq existed, and a reader's existing
// configuration file, which names no provider Groq did not exist to be
// named in, must keep working exactly as it did.
func isGroqProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), groqProviderName)
}
