package openrouter

import "context"

// KeyInfo is the usage the endpoint reports against the credential this client
// holds.
//
// Limit and LimitRemaining are pointers for the reason [Usage]'s own fields are:
// a key with no limit set and a key the endpoint reports as exhausted (a limit of
// zero remaining) are different states, and a plain float cannot tell them apart.
// A caller that read a zero would not know whether the reader should be told
// "there is no cap" or "spend any more and the next request is refused."
type KeyInfo struct {
	// Label is the name the reader gave this key when it was created, for a
	// report that names what is being described rather than only its numbers.
	Label string `json:"label"`

	// Usage is the dollar amount spent against this key so far.
	Usage float64 `json:"usage"`

	// Limit is the dollar cap on this key, or nil for a key with none set.
	Limit *float64 `json:"limit"`

	// LimitRemaining is Limit minus Usage, carried as its own field rather than
	// computed here, since the endpoint is the one place that knows whether a
	// credit or a promotional balance moves the figure in a way a plain
	// subtraction would get wrong.
	LimitRemaining *float64 `json:"limit_remaining"`

	// IsFreeTier reports whether this key is restricted to the endpoint's free
	// models, which /key's report names so a reader is not left to work out why
	// a paid model they tried was refused.
	IsFreeTier bool `json:"is_free_tier"`
}

// keyResponse is the envelope /auth/key answers with, the same "data" wrapper
// [modelsResponse] unwraps for /models.
type keyResponse struct {
	Data KeyInfo `json:"data"`
}

// KeyUsage reports the endpoint's own accounting for the credential this client
// holds.
//
// It answers the question /key exists to ask - what has this credential spent,
// and what is left - by reading the one endpoint whose job is that question,
// rather than this client trying to keep a running total across requests: a
// total kept here would start at zero every time the program restarts and would
// disagree with the endpoint's own count the moment a second client, or the
// reader's own dashboard, made a request this one did not see.
func (c *Client) KeyUsage(ctx context.Context) (KeyInfo, error) {
	var out keyResponse
	if err := c.get(ctx, "/auth/key", &out); err != nil {
		return KeyInfo{}, err
	}
	return out.Data, nil
}
