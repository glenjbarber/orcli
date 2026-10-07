package groq

import (
	"context"
	"errors"

	"github.com/glenjbarber/orcli/internal/openrouter"
)

// ErrKeyUsageUnsupported is returned by [Client.KeyUsage].
//
// OpenRouter publishes /auth/key, the endpoint [openrouter.Client.KeyUsage]
// reads to answer what a credential has spent and what is left. Groq
// publishes no equivalent endpoint: there is nothing documented for this
// client to call that reports usage or a remaining limit against an API key.
// Returning this error, rather than a KeyInfo decoded from a guessed-at shape
// or a silently zeroed one, is what keeps a reader of /key's output - or of
// this package - from mistaking "Groq has no such endpoint" for "the
// request failed" or, worse, for "this key has spent nothing."
var ErrKeyUsageUnsupported = errors.New("groq: the endpoint publishes no key usage accounting")

// KeyUsage reports the endpoint's own accounting for the credential this
// client holds.
//
// It always fails with [ErrKeyUsageUnsupported]. The method exists at all so
// *Client keeps satisfying the same catalogClient interface
// [*openrouter.Client] does in cmd/orcli (see cmd/orcli/openrouter_cmds.go),
// which is what lets /key, /models and /freemodels be wired to whichever
// provider a session is configured for without cmd/orcli needing a second,
// narrower interface for the provider that cannot answer every call the
// first one can. A caller of /key against a Groq-configured session is told
// this in the one place /key already reports a failure, rather than cmd/orcli
// growing a special case to ask in advance which provider supports which
// command.
func (c *Client) KeyUsage(ctx context.Context) (openrouter.KeyInfo, error) {
	return openrouter.KeyInfo{}, ErrKeyUsageUnsupported
}
