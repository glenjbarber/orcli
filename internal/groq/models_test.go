package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// servingGET returns a Client pointed at a test server that answers every
// GET with the given status and body, and reports the path and the
// Authorization header the server saw, mirroring
// internal/openrouter/models_test.go's own servingGET.
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

	c := New("gsk-test")
	c.baseURL = srv.URL

	return c, &gotPath, &gotAuth
}

// TestModelsDecodesWhatGroqActuallySends is the test this package's whole
// no-hard-coded-model-names constraint rests on: the identifiers this method
// returns come from the server's own response body, which this test
// controls and can change at will, never from a list this package compiles
// in. Changing the body between two calls, with no change to this package's
// own source, and getting back two different lists is the proof the
// constraint is honored.
func TestModelsDecodesWhatGroqActuallySends(t *testing.T) {
	c, path, auth := servingGET(t, http.StatusOK,
		`{"data":[{"id":"alpha-model","object":"model","owned_by":"groq"},`+
			`{"id":"beta-model","object":"model","owned_by":"groq"}]}`)

	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].ID != "alpha-model" || models[0].Name != "alpha-model" {
		t.Errorf("first model decoded as %+v", models[0])
	}
	if models[1].ID != "beta-model" {
		t.Errorf("second model decoded as %+v", models[1])
	}
	if *path != "/models" {
		t.Errorf("the request hit %q, want /models", *path)
	}
	if !strings.Contains(*auth, "gsk-test") {
		t.Errorf("the request did not carry the credential: %q", *auth)
	}
}

// TestModelsReflectsAChangedCatalog proves the point TestModelsDecodesWhat-
// GroqActuallySends's own doc comment makes explicit: a model that
// disappears from Groq's free tier disappears from this method's result on
// the very next call, because nothing about the list is remembered between
// calls or compiled into this package.
func TestModelsReflectsAChangedCatalog(t *testing.T) {
	c, _, _ := servingGET(t, http.StatusOK, `{"data":[{"id":"was-here-yesterday"}]}`)

	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "was-here-yesterday" {
		t.Fatalf("got %+v, want one model named was-here-yesterday", models)
	}
}

// TestModelsFreeIsTrueForEveryGroqModel covers the consequence
// [Client.Models]'s own doc comment names: Groq's response carries no
// pricing block, so every model it returns decodes to a zero Pricing, and
// [openrouter.ModelInfo.Free] already treats that as free - the correct
// reading for a catalog that is, today, entirely Glen's free-tier account.
func TestModelsFreeIsTrueForEveryGroqModel(t *testing.T) {
	c, _, _ := servingGET(t, http.StatusOK, `{"data":[{"id":"any-model"}]}`)

	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	if !models[0].Free() {
		t.Errorf("model %+v did not report free, want true for an unpriced Groq model", models[0])
	}
}

// TestModelsReportsAnEndpointRefusal mirrors
// internal/openrouter/models_test.go's own test of the same name: the
// caller is told what the endpoint said rather than handed a decode failure
// over an error body.
func TestModelsReportsAnEndpointRefusal(t *testing.T) {
	c, _, _ := servingGET(t, http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`)

	_, err := c.Models(context.Background())
	if err == nil {
		t.Fatal("a 401 was reported as a success")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("the error is %q, want it to quote the endpoint", err)
	}
}

// TestModelsWithNoKeyIsRefusedLocally covers the same rule
// [Client.Chat] already enforces: a request with no credential is caught
// here rather than sent.
func TestModelsWithNoKeyIsRefusedLocally(t *testing.T) {
	c := New("")
	_, err := c.Models(context.Background())
	if err != ErrNoAPIKey {
		t.Errorf("Models with no key returned %v, want ErrNoAPIKey", err)
	}
}
