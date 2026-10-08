// Package groq is a client for the Groq API.
//
// Groq's chat and models endpoints are OpenAI-compatible, the same wire shape
// OpenRouter's own endpoints already are, so this package is deliberately a
// second transport built to the same contract [github.com/glenjbarber/orcli/internal/openrouter]
// keeps rather than a transport with its own request and reply types.
//
// # Why this package imports internal/openrouter rather than defining its own types
//
// Request, Message, Tool, ToolCall, Event, EventKind and Usage are read from
// internal/openrouter rather than redeclared here. Both endpoints speak the
// same /chat/completions body and the same Server-Sent Events stream shape,
// down to the field names a chunk carries, so a second copy of those types
// would not describe a different wire format - it would describe the same one
// a second time, and the two copies would drift the moment one endpoint's
// client was extended to read a field the other already has. cmd/orcli's own
// chatClient interface (see cmd/orcli/ask.go) already names openrouter.Request
// and openrouter.Event in its signature, which is the second reason this
// package reuses them rather than its own: a *Client here satisfies that
// interface with no change to it and no second interface for ask to accept,
// since Chat's signature is identical either way.
//
// ModelInfo is reused for the same reason, with one asymmetry this package's
// own doc comments name where it matters: OpenRouter's /models answers with a
// display name and a price per model, and Groq's does not. [Client.Models]
// decodes what Groq actually sends and leaves the fields Groq's endpoint is
// silent about at their zero value, which is what makes every model Groq
// offers report free - true of a provider whose whole offering, at the time
// this package was written, is Glen's free-tier account, and exactly what
// [internal/openrouter.ModelInfo.Free] already does for a model that carries no
// price at all.
//
// What this package does not reuse is the credential-usage endpoint. Groq
// publishes no endpoint equivalent to OpenRouter's /auth/key, so
// [Client.KeyUsage] returns [ErrKeyUsageUnsupported] rather than guessing at a
// shape to decode, which keeps *Client satisfying the same catalogClient
// interface cmd/orcli already has without that interface claiming an
// accounting figure this provider has never been asked to produce.
//
// This client, like internal/openrouter's, is standard library only, and
// keeps the same property that package exists to keep: a stream cut short is
// never presented as a complete reply, and the text that arrived before a
// failure is never thrown away with the failure.
package groq
