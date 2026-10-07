package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// servingGET returns a Client pointed at a test server that answers every GET
// with the given status and body, and reports the path and the Authorization
// header the server saw.
func servingGET(t *testing.T, status int, body string) (*Client, *string, *string) {
	t.Helper()

	var gotPath, gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := New("sk-or-v1-test")
	c.baseURL = srv.URL

	return c, &gotPath, &gotAuth
}

// TestModelsDecodesTheCatalog covers the plain case: a well-formed body becomes
// the list a caller can filter.
func TestModelsDecodesTheCatalog(t *testing.T) {
	c, path, auth := servingGET(t, http.StatusOK, `{"data":[
		{"id":"vendor/a","name":"A","pricing":{"prompt":"0.000001","completion":"0.000002"}},
		{"id":"vendor/b:free","name":"B (free)","pricing":{"prompt":"0","completion":"0"}}
	]}`)

	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].ID != "vendor/a" || models[0].Free() {
		t.Errorf("vendor/a decoded as %+v, want a priced model named vendor/a", models[0])
	}
	if models[1].ID != "vendor/b:free" || !models[1].Free() {
		t.Errorf("vendor/b:free decoded as %+v, want a free model", models[1])
	}
	if *path != "/models" {
		t.Errorf("the request hit %q, want /models", *path)
	}
	if !strings.Contains(*auth, "sk-or-v1-test") {
		t.Errorf("the request did not carry the credential: %q", *auth)
	}
}

// TestModelsFreeHandlesEveryZeroSpelling covers the point [ModelInfo.Free]'s own
// doc comment makes: the endpoint can spell zero several ways.
func TestModelsFreeHandlesEveryZeroSpelling(t *testing.T) {
	for _, tc := range []struct {
		prompt, completion string
		free               bool
	}{
		{"0", "0", true},
		{"0.0", "0.0", true},
		{"", "", true},
		{"0.000001", "0", false},
		{"0", "0.000002", false},
	} {
		m := ModelInfo{Pricing: Pricing{Prompt: tc.prompt, Completion: tc.completion}}
		if got := m.Free(); got != tc.free {
			t.Errorf("Pricing{%q,%q}.Free() = %v, want %v", tc.prompt, tc.completion, got, tc.free)
		}
	}
}

// TestModelsReportsAnEndpointRefusal covers the non-200 path: the caller is told
// what the endpoint said rather than handed a decode failure over an error body.
func TestModelsReportsAnEndpointRefusal(t *testing.T) {
	c, _, _ := servingGET(t, http.StatusUnauthorized, `{"error":{"message":"invalid credential"}}`)

	_, err := c.Models(context.Background())
	if err == nil {
		t.Fatal("a 401 was reported as a success")
	}
	if !strings.Contains(err.Error(), "invalid credential") {
		t.Errorf("the error is %q, want it to quote the endpoint", err)
	}
}

// TestModelsWithNoKeyIsRefusedLocally covers the same rule [Client.Chat] already
// enforces: a request with no credential is caught here rather than sent, since
// the backend cannot tell a missing key from an invalid one.
func TestModelsWithNoKeyIsRefusedLocally(t *testing.T) {
	c := New("")
	_, err := c.Models(context.Background())
	if err != ErrNoAPIKey {
		t.Errorf("Models with no key returned %v, want ErrNoAPIKey", err)
	}
}

// TestKeyUsageDecodesTheAccounting covers /auth/key's envelope and the pointer
// fields that tell an absent limit apart from an exhausted one.
func TestKeyUsageDecodesTheAccounting(t *testing.T) {
	c, path, _ := servingGET(t, http.StatusOK,
		`{"data":{"label":"orcli","usage":1.5,"limit":10,"limit_remaining":8.5,"is_free_tier":false}}`)

	info, err := c.KeyUsage(context.Background())
	if err != nil {
		t.Fatalf("KeyUsage: %v", err)
	}
	if info.Label != "orcli" || info.Usage != 1.5 {
		t.Errorf("KeyUsage decoded as %+v", info)
	}
	if info.Limit == nil || *info.Limit != 10 {
		t.Errorf("Limit decoded as %v, want 10", info.Limit)
	}
	if info.LimitRemaining == nil || *info.LimitRemaining != 8.5 {
		t.Errorf("LimitRemaining decoded as %v, want 8.5", info.LimitRemaining)
	}
	if *path != "/auth/key" {
		t.Errorf("the request hit %q, want /auth/key", *path)
	}
}

// TestKeyUsageWithNoLimitLeavesItNil covers the key with no cap, which is a
// different state than a cap already spent, and only a pointer tells them apart.
func TestKeyUsageWithNoLimitLeavesItNil(t *testing.T) {
	c, _, _ := servingGET(t, http.StatusOK,
		`{"data":{"label":"orcli","usage":1.5,"limit":null,"limit_remaining":null,"is_free_tier":false}}`)

	info, err := c.KeyUsage(context.Background())
	if err != nil {
		t.Fatalf("KeyUsage: %v", err)
	}
	if info.Limit != nil {
		t.Errorf("Limit is %v, want nil for a key with no cap", info.Limit)
	}
}

// TestKeyUsageReportsAnEndpointRefusal mirrors TestModelsReportsAnEndpointRefusal
// for the second GET this file adds.
func TestKeyUsageReportsAnEndpointRefusal(t *testing.T) {
	c, _, _ := servingGET(t, http.StatusForbidden, `{"error":{"message":"key revoked"}}`)

	_, err := c.KeyUsage(context.Background())
	if err == nil {
		t.Fatal("a 403 was reported as a success")
	}
	if !strings.Contains(err.Error(), "key revoked") {
		t.Errorf("the error is %q, want it to quote the endpoint", err)
	}
}
