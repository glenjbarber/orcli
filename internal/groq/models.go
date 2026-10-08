package groq

import (
	"context"

	"github.com/glenjbarber/orcli/internal/openrouter"
)

// modelWire is one model as Groq's own /models answers it.
//
// This is deliberately not [openrouter.ModelInfo]: Groq's object carries an
// id, the endpoint's own object/created/owned_by/active bookkeeping, and a
// context_window figure, and it carries neither a display name nor a pricing
// block. Decoding straight into openrouter.ModelInfo would silently read
// nothing for Name and Pricing - both fields this struct does not have a
// same-named counterpart for - and leave a reader of this file to wonder
// whether that absence was checked or missed. Naming the wire shape Groq
// actually sends, even though only ID survives into the type this package
// returns, is what [Client.Models]'s own doc comment can then point at to
// explain the gap by name instead of leaving it implicit.
//
// Nothing here names a model identifier. That is the one constraint this
// package exists under: Groq has removed models from its free tier before,
// per Glen, and a model id compiled into this file as a default or an
// example would be exactly the kind of value that goes stale the next time
// that happens. The catalog is read fresh from Groq's own endpoint every
// time [Client.Models] is called, and nowhere else in this package is a
// model identifier written down.
type modelWire struct {
	ID string `json:"id"`
}

// modelsResponse is the envelope /models answers with, the same "data"
// wrapper [openrouter.Client.Models] already unwraps for OpenRouter's own
// catalog call.
type modelsResponse struct {
	Data []modelWire `json:"data"`
}

// Models lists the models Groq currently offers.
//
// It returns [openrouter.ModelInfo] for the reason [New]'s own package doc
// explains at length: cmd/orcli's catalogClient interface already names that
// type, and a second, Groq-specific model type would force a second
// interface, or a type switch in cmd/orcli, for no difference in what either
// endpoint's catalog is used for - choosing a model to send a request to and
// deciding whether it is free.
//
// ModelInfo.Name is set to the identifier, since Groq's response carries no
// separate display name and a blank Name would read, to /models' own
// renderer, as a model this client half-decoded rather than one the endpoint
// simply does not name twice. ModelInfo.Pricing is left at its zero value,
// since Groq's response carries no price either - and
// [openrouter.ModelInfo.Free] already treats an absent price as free, which
// is the correct answer for a catalog this package exists to read because
// every model behind it is Glen's free-tier account.
//
// Models never hard-codes a model identifier anywhere, on the same grounds
// [modelWire]'s own doc comment names: every identifier this method returns
// came from Groq's own response to this call, not from a list compiled into
// this binary that a removed free-tier model could make wrong.
func (c *Client) Models(ctx context.Context) ([]openrouter.ModelInfo, error) {
	var out modelsResponse
	if err := c.get(ctx, "/models", &out); err != nil {
		return nil, err
	}

	models := make([]openrouter.ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		models = append(models, openrouter.ModelInfo{ID: m.ID, Name: m.ID})
	}
	return models, nil
}
