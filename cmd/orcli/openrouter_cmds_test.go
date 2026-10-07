package main

import (
	"context"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tui"
)

// fakeCatalog is the in-memory catalogClient every test in this file hands the
// dispatcher, on the grounds catalogClient's own doc comment gives: openrouter.
// Client's base URL cannot be pointed at a test server from outside its package.
type fakeCatalog struct {
	models    []openrouter.ModelInfo
	modelsErr error
	key       openrouter.KeyInfo
	keyErr    error
}

func (f *fakeCatalog) Models(ctx context.Context) ([]openrouter.ModelInfo, error) {
	return f.models, f.modelsErr
}

func (f *fakeCatalog) KeyUsage(ctx context.Context) (openrouter.KeyInfo, error) {
	return f.key, f.keyErr
}

// overCatalog returns a dispatcher whose OpenRouter calls answer from fake rather
// than a real transport, over a session so /search and /attribute have one to
// read and write.
func overCatalog(t *testing.T, fake *fakeCatalog) *dispatcher {
	t.Helper()

	d := newDispatcherFor(config.Config{APIKey: "k"})
	d.newORClient = func(key string) catalogClient { return fake }
	d.withSession(tui.New(tui.Options{Model: "first/model", APIKey: "k"}))
	return d
}

func runCmd(t *testing.T, d *dispatcher, line string) tui.Result {
	t.Helper()

	out, err := d.Run(context.Background(), line)
	if err != nil {
		t.Fatalf("%s: %v", line, err)
	}
	return out
}

// TestKeyReportsTheEndpointsOwnUsage covers the plain case: the figures
// KeyUsage returns are the figures the reply carries.
func TestKeyReportsTheEndpointsOwnUsage(t *testing.T) {
	limit := 10.0
	remaining := 8.5
	d := overCatalog(t, &fakeCatalog{key: openrouter.KeyInfo{
		Label: "orcli", Usage: 1.5, Limit: &limit, LimitRemaining: &remaining,
	}})

	out := runCmd(t, d, "/key")
	for _, want := range []string{"orcli", "1.5000", "10.0000", "8.5000"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the report does not carry %q: %q", want, out.Text)
		}
	}
}

// TestKeyWithNoLimitSaysSo covers the pointer: a key with no cap is reported as
// having none, not as a cap of zero.
func TestKeyWithNoLimitSaysSo(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{key: openrouter.KeyInfo{Label: "orcli", Usage: 0.2}})

	out := runCmd(t, d, "/key")
	if !strings.Contains(out.Text, "no limit set") {
		t.Errorf("the report is %q, want it to say there is no limit", out.Text)
	}
}

// TestKeyWithNoCredentialIsRefused covers the first-run state: no call is made
// and the reader is told why.
func TestKeyWithNoCredentialIsRefused(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{}))

	_, err := d.Run(context.Background(), "/key")
	if err == nil {
		t.Fatal("/key with no credential was accepted")
	}
}

// modelsFixture is the catalog every listing test filters.
var modelsFixture = []openrouter.ModelInfo{
	{ID: "vendor/paid", Name: "Paid Model", Pricing: openrouter.Pricing{Prompt: "0.000001", Completion: "0.000002"}},
	{ID: "vendor/free", Name: "Free Model", Pricing: openrouter.Pricing{Prompt: "0", Completion: "0"}},
	{ID: "other/paid", Name: "Other Paid", Pricing: openrouter.Pricing{Prompt: "0.00001", Completion: "0.00002"}},
}

// TestModelsListsEveryModelUnfiltered covers /models with no argument.
func TestModelsListsEveryModelUnfiltered(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{models: modelsFixture})

	out := runCmd(t, d, "/models")
	for _, want := range []string{"vendor/paid", "vendor/free", "other/paid"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the listing does not carry %q:\n%s", want, out.Text)
		}
	}
}

// TestModelsFiltersAsTyped covers the filter: a reader typing part of a name or
// identifier narrows the listing to it.
func TestModelsFiltersAsTyped(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{models: modelsFixture})

	out := runCmd(t, d, "/models vendor")
	if !strings.Contains(out.Text, "vendor/paid") || !strings.Contains(out.Text, "vendor/free") {
		t.Errorf("the filtered listing is missing a vendor model:\n%s", out.Text)
	}
	if strings.Contains(out.Text, "other/paid") {
		t.Errorf("the filter did not exclude other/paid:\n%s", out.Text)
	}
}

// TestFreemodelsListsOnlyZeroPriced covers the rule that free is decided by the
// endpoint's own pricing, through ModelInfo.Free, not by a naming guess.
func TestFreemodelsListsOnlyZeroPriced(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{models: modelsFixture})

	out := runCmd(t, d, "/freemodels")
	if !strings.Contains(out.Text, "vendor/free") {
		t.Errorf("the free listing is missing vendor/free:\n%s", out.Text)
	}
	for _, unwanted := range []string{"vendor/paid", "other/paid"} {
		if strings.Contains(out.Text, unwanted) {
			t.Errorf("the free listing carries a priced model %q:\n%s", unwanted, out.Text)
		}
	}
}

// TestFreemodelsWithNoMatchSaysSo covers the empty case, so a reader filtering
// past every free model is told rather than shown a blank reply.
func TestFreemodelsWithNoMatchSaysSo(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{models: modelsFixture})

	out := runCmd(t, d, "/freemodels nosuch")
	if !strings.Contains(out.Text, "no free models match") {
		t.Errorf("the reply is %q, want it to say nothing matched", out.Text)
	}
}

// TestModelsReportsAnEndpointFailure covers the refusal path: a fetch that fails
// is a result the reader sees named, not a blank listing.
func TestModelsReportsAnEndpointFailure(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{modelsErr: openrouter.ErrNoAPIKey})

	_, err := d.Run(context.Background(), "/models")
	if err == nil {
		t.Fatal("a failed catalog fetch was accepted")
	}
}

// TestSearchFindsAQuestionAlreadyInTheLog covers the ordinary case /search exists
// for: filtering what is already written rather than asking the network anything.
func TestSearchFindsAQuestionAlreadyInTheLog(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{})
	d.session.Deliver("the quick brown fox", 0)
	d.session.Notice("an unrelated notice", 0, tui.RoleDim)

	out := runCmd(t, d, "/search brown")
	if !strings.Contains(out.Text, "quick brown fox") {
		t.Errorf("the search result is %q, want the matching row", out.Text)
	}
	if strings.Contains(out.Text, "unrelated") {
		t.Errorf("the search result carries a row that does not match: %q", out.Text)
	}
}

// TestSearchIsCaseInsensitive covers the comparison /search makes, the same one
// /models applies to the catalog.
func TestSearchIsCaseInsensitive(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{})
	d.session.Deliver("The Quick Brown Fox", 0)

	out := runCmd(t, d, "/search BROWN")
	if !strings.Contains(out.Text, "Quick Brown Fox") {
		t.Errorf("the search result is %q, want a case-insensitive match", out.Text)
	}
}

// TestSearchWithNoMatchSaysSo covers the empty case.
func TestSearchWithNoMatchSaysSo(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{})
	d.session.Deliver("the quick brown fox", 0)

	out := runCmd(t, d, "/search nosuchword")
	if !strings.Contains(out.Text, "no rows match") {
		t.Errorf("the reply is %q, want it to say nothing matched", out.Text)
	}
}

// TestSearchWithNoTextIsRefused covers the reader who typed the command with
// nothing to filter on.
func TestSearchWithNoTextIsRefused(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{})

	_, err := d.Run(context.Background(), "/search")
	if err == nil {
		t.Fatal("/search with no text was accepted")
	}
}

// TestSearchMakesNoNetworkCall covers the design point itself: a fake that
// answers Models or KeyUsage would fail this test by never being asked to.
func TestSearchMakesNoNetworkCall(t *testing.T) {
	fake := &fakeCatalog{modelsErr: errAlwaysFails, keyErr: errAlwaysFails}
	d := overCatalog(t, fake)
	d.session.Deliver("reachable text", 0)

	if _, err := d.Run(context.Background(), "/search reachable"); err != nil {
		t.Fatalf("/search reached the network: %v", err)
	}
}

// TestAttributeSetsAModelTheCatalogOffers covers the promise /attribute makes
// that /model does not: the value is checked against a live list first.
func TestAttributeSetsAModelTheCatalogOffers(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{models: modelsFixture})

	out := runCmd(t, d, "/attribute vendor/free")
	if !strings.Contains(out.Text, "vendor/free") {
		t.Errorf("the reply is %q, want it to name the model", out.Text)
	}
	if got := d.session.Options().Model; got != "vendor/free" {
		t.Errorf("the session answers with %q, want vendor/free", got)
	}
}

// TestAttributeRefusesAModelNotOffered covers the stricter check: a name the
// catalog does not carry is refused before anything is set, and the session
// keeps answering with what it had.
func TestAttributeRefusesAModelNotOffered(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{models: modelsFixture})

	_, err := d.Run(context.Background(), "/attribute nosuch/model")
	if err == nil {
		t.Fatal("/attribute accepted a model the catalog does not offer")
	}
	if !strings.Contains(err.Error(), "nosuch/model") {
		t.Errorf("the refusal is %q, want it to name what was typed", err)
	}
	if got := d.session.Options().Model; got != "first/model" {
		t.Errorf("the session answers with %q, want the model it started with", got)
	}
}

// TestAttributeDoesNotWriteTheConfigurationFile covers the contrast with /model:
// /attribute changes only what this session answers with.
func TestAttributeDoesNotWriteTheConfigurationFile(t *testing.T) {
	calls := 0
	oldSwap := writeModelSwap
	writeModelSwap = func(path, model string) error {
		calls++
		return oldSwap(path, model)
	}
	t.Cleanup(func() { writeModelSwap = oldSwap })

	d := overCatalog(t, &fakeCatalog{models: modelsFixture})
	runCmd(t, d, "/attribute vendor/free")

	if calls != 0 {
		t.Errorf("/attribute wrote the configuration file %d times, want 0", calls)
	}
}

// TestAttributeWithNoArgumentReportsTheCurrentModel mirrors /model's own report
// case, without the "last" member /attribute never writes.
func TestAttributeWithNoArgumentReportsTheCurrentModel(t *testing.T) {
	d := overCatalog(t, &fakeCatalog{})

	out := runCmd(t, d, "/attribute")
	if !strings.Contains(out.Text, "first/model") {
		t.Errorf("the report is %q, want it to name the current model", out.Text)
	}
}

// errAlwaysFails is a sentinel error for tests that must never see it returned.
var errAlwaysFails = &sentinelError{"this call should never be made"}

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }
