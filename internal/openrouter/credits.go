package openrouter

import "context"

// CreditInfo is the account-level balance returned by OpenRouter's management
// credits endpoint. It is separate from per-key limits reported by /auth/key.
type CreditInfo struct {
	TotalCredits float64 `json:"total_credits"`
	TotalUsage   float64 `json:"total_usage"`
}

type creditsResponse struct {
	Data CreditInfo `json:"data"`
}

// Credits reports the remaining account balance. OpenRouter requires a
// management key for this endpoint; callers should treat refusal as unavailable.
func (c *Client) Credits(ctx context.Context) (CreditInfo, error) {
	var out creditsResponse
	if err := c.get(ctx, "/credits", &out); err != nil {
		return CreditInfo{}, err
	}
	return out.Data, nil
}

// Remaining is the purchased balance less account usage.
func (c CreditInfo) Remaining() float64 { return c.TotalCredits - c.TotalUsage }
