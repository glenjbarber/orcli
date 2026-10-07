package openrouter

import (
	"context"
	"fmt"
)

// ModelInfo is one model the endpoint offers, as /models reports it.
//
// It carries only what this client reads today: the identifier a request names in
// Request.Model, the display name a reader recognizes, and the pricing a reader
// weighs before choosing one. The endpoint's own response carries more
// (context_length, architecture, top_provider and the rest), and none of it is
// decoded here, on the same grounds [Message] and [Request] already keep: a field
// this client never reads is a field whose shape can change upstream without this
// client noticing, so only what is used is named.
type ModelInfo struct {
	// ID is what a request names in Request.Model. It is the identifier, not the
	// display name, because that is the value this client's own callers - /model
	// and /attribute - have to compare against and send back.
	ID string `json:"id"`

	// Name is the display name a reader reads in a listing. It is kept separate
	// from ID because the two differ for most models the endpoint carries, and a
	// listing that only showed the identifier would be a listing a reader cannot
	// scan.
	Name string `json:"name"`

	// Pricing is what the endpoint charges per token for this model. It decides
	// what [ModelInfo.Free] answers, which is the one thing /freemodels exists to
	// filter on.
	Pricing Pricing `json:"pricing"`
}

// Pricing is the per-token cost of one model, as the endpoint reports it.
//
// Both figures arrive as strings - decimal fractions of a dollar per token, small
// enough that a float would round some of them to the same bits as zero - and this
// client keeps them as strings for the same reason the rest of the package keeps a
// credential as a string rather than a numeric type: a value this client only
// compares and displays does not need to become a number to do either.
type Pricing struct {
	// Prompt is the cost per prompt token.
	Prompt string `json:"prompt"`
	// Completion is the cost per completion token.
	Completion string `json:"completion"`
}

// Free reports whether a model costs nothing to call.
//
// The endpoint's own catalog is what decides this, not a heuristic over the name:
// a model whose identifier ends in ":free" is the common case, but the rule this
// method applies is the one the endpoint's own pricing block states, which is "0"
// (or empty, for a model that quotes no price at all) on both figures. A reader
// filtering on a guessed-at naming convention would miss a model the endpoint
// prices at zero under a name that does not say so, and would wrongly admit one
// that happens to end in ":free" but whose price changed.
func (m ModelInfo) Free() bool {
	return isZeroPrice(m.Pricing.Prompt) && isZeroPrice(m.Pricing.Completion)
}

// isZeroPrice reports whether a pricing figure is absent or exactly zero.
//
// The endpoint's decimal strings vary in how they spell zero - "0", "0.0", an
// empty string for a model that carries no figure at all - so this compares the
// parsed value rather than the spelling.
func isZeroPrice(s string) bool {
	if s == "" {
		return true
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil {
		return false
	}
	return f == 0
}

// modelsResponse is the envelope /models answers with.
//
// The endpoint wraps the list in a "data" member rather than returning it bare, the
// same shape [Client.Chat]'s own reply wraps choices in, and this struct exists only
// to be unwrapped once, here, so nothing past this file has to know the envelope is
// there.
type modelsResponse struct {
	Data []ModelInfo `json:"data"`
}

// Models lists the models the endpoint currently offers.
//
// It is a plain GET, not a stream: the endpoint answers the whole catalog in one
// JSON body, and there is nothing here for [Client.Chat]'s retry-on-undelivered
// rule to apply to - a catalog request either answers or it does not, with no
// partial text to preserve across an attempt. A failure is returned rather than
// delivered through a callback, unlike Chat, because there is no partial reply a
// caller would lose by also losing the error: the whole point of this call is the
// list, and a list that did not arrive is nothing for the caller to show in its
// place but the error itself.
//
// This is the one call this client makes with no credential strictly required by
// the endpoint - the catalog is public - but the bearer header is sent anyway, on
// the same grounds [Client.Chat] always sends it: a caller that already holds a
// Client holds it because a key was configured, and a second code path that
// skipped the header would be a second thing to keep in step should the endpoint
// ever start pricing the catalog call per key.
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	var out modelsResponse
	if err := c.get(ctx, "/models", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
