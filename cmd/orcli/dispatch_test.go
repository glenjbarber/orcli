package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glenjbarber/orcli/internal/cloudflare"
	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// zoneBody is a zone holding one record, which is the shape a list call decodes.
const zoneBody = `{"success":true,"result":[
	{"id":"rec1","type":"A","name":"app.example.com","content":"198.51.100.7","ttl":300,"proxied":false}
]}`

// emptyZone is a zone with nothing in it.
const emptyZone = `{"success":true,"result":[]}`

// over builds a dispatcher over a configuration file body, with the API pointed at
// the given handler.
//
// The configuration is written to a real file and read back through Parse rather
// than being built as a struct, since the block reader is one of the things under
// test and a struct would bypass it.
func over(t *testing.T, body string, handler http.HandlerFunc) *dispatcher {
	t.Helper()

	cfg := loadBody(t, body)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	d := newDispatcherFor(cfg)
	d.newClient = func(key string) *cloudflare.APIClient {
		c := cloudflare.New(key)
		c.Base = srv.URL
		return c
	}
	return d
}

// TestTheCommandIsFoundAndRun covers the seam itself, since a dispatcher no line
// reaches is the state the tree was in before this unit.
func TestTheCommandIsFoundAndRun(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(zoneBody))
		})

	out, err := d.Run(context.Background(), "/cloudflare dns list --zone example.com")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "app.example.com") {
		t.Errorf("the list does not carry the record:\n%s", out.Text)
	}
}

func TestEveryListedCommandAcceptsHelp(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	for _, name := range tui.Names() {
		t.Run(name, func(t *testing.T) {
			out, err := d.Run(context.Background(), "/"+name+" help")
			if err != nil {
				t.Fatalf("help: %v", err)
			}
			command, _ := tui.Lookup(name)
			if !strings.Contains(out.Text, "/"+command.Name) || out.Ask != "" {
				t.Fatalf("help result = %+v", out)
			}
		})
	}
}

func TestPluginHelpAsksModelForSafeSetupGuidance(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	out, err := d.Run(context.Background(), "@notion help")
	if err != nil {
		t.Fatalf("plugin help: %v", err)
	}
	if out.Ask == "" || out.Text != "" {
		t.Fatalf("plugin help result = %+v, want a model instruction", out)
	}
	for _, want := range []string{"api_key", "~/.orcli.json", "0600", "placeholder", "Do not ask for"} {
		if !strings.Contains(out.Ask, want) {
			t.Errorf("help prompt does not include %q", want)
		}
	}
	for _, secret := range []string{"ntn_", "secret_", "sk-"} {
		if strings.Contains(out.Ask, secret) {
			t.Errorf("help prompt contains token-like value %q", secret)
		}
	}
}

func TestPluginSubcommandSelectsToolOperation(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	out, err := d.Run(context.Background(), "@notion search query")
	if err != nil {
		t.Fatalf("plugin operation: %v", err)
	}
	if !strings.Contains(out.Ask, "@notion") || !strings.Contains(out.Ask, "search") || !strings.Contains(out.Ask, "query") {
		t.Fatalf("plugin operation prompt = %q", out.Ask)
	}
}

func TestPluginTriggerForwardsFreeFormTaskToTheNamedPlugin(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	out, err := d.Run(context.Background(), "@notion find the roadmap page")
	if err != nil {
		t.Fatalf("plugin trigger: %v", err)
	}
	for _, want := range []string{"explicitly requested @notion", "instruction to use the notion plugin", "find the roadmap page", "notion plugin tools"} {
		if !strings.Contains(strings.ToLower(out.Ask), strings.ToLower(want)) {
			t.Errorf("plugin request prompt %q does not include %q", out.Ask, want)
		}
	}
}

func TestBarePluginTriggerAsksWhatTheUserWants(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	out, err := d.Run(context.Background(), "@notion")
	if err != nil {
		t.Fatalf("bare plugin trigger: %v", err)
	}
	if !strings.Contains(out.Ask, "Ask what they would like done with Notion") {
		t.Fatalf("bare plugin prompt = %q", out.Ask)
	}
}

func TestUnconfiguredApiaryActionRequestsSetupGuidance(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	out, err := d.Run(context.Background(), "@apiary query status")
	if err != nil {
		t.Fatalf("Apiary query: %v", err)
	}
	if !strings.Contains(out.Ask, "not configured") || !strings.Contains(out.Ask, "viewer_token") {
		t.Fatalf("Apiary setup prompt = %q", out.Ask)
	}
}

// TestALineThatIsNotACommandIsNotARefusal covers the ordinary case. A question the
// reader wants to ask is not a command, and the loop is what sends it to the model.
func TestALineThatIsNotACommandIsNotARefusal(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	out, err := d.Run(context.Background(), "what is in DESIGN.md")
	if err != nil {
		t.Fatalf("a question was refused: %v", err)
	}
	if out.Text != "" || out.Ask != "" || out.Quit {
		t.Errorf("a question produced a result: %+v", out)
	}
}

// TestQuitIsTheOneResultThatAsksTheLoopToLeave covers the flag the loop reads, since
// a command that returned it by accident would close the interface on a reader who
// typed something else.
func TestQuitIsTheOneResultThatAsksTheLoopToLeave(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	out, err := d.Run(context.Background(), "/quit")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.Quit {
		t.Error("/quit did not ask the loop to leave")
	}
	if out.Text != "" || out.Ask != "" {
		t.Errorf("/quit produced %+v, want only the flag", out)
	}
}

// TestACommandInTheTableButNotRunIsToldApartFromAnUnknownOne covers the case a
// reader meets on day one: the table names more commands than this build runs.
// /compact is used here as one still unbuilt - the OpenRouter transport cluster
// (/key, /search, /models, /freemodels, /attribute) that used to leave /search as
// the last name in this state is wired in now; it is not the point of the test,
// only a name this build does not yet have a handler for.
func TestACommandInTheTableButNotRunIsToldApartFromAnUnknownOne(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	_, err := d.Run(context.Background(), "/compact")
	if err == nil {
		t.Fatal("a command this build does not run was accepted")
	}
	if !strings.Contains(err.Error(), "table") {
		t.Errorf("the refusal is %q, want it to say the name is in the table", err)
	}

	_, err = d.Run(context.Background(), "/nonesuch")
	if err == nil {
		t.Fatal("an unknown command was accepted")
	}
	if strings.Contains(err.Error(), "table") {
		t.Errorf("an unknown command was reported as one in the table: %q", err)
	}
}

// TestAnUnknownCommandCloseToARealOneGetsASuggestion covers the worked example this
// feature was asked for: a typo that shares no prefix with the command it was meant
// to be, so the Tab completer's prefix matching in internal/tui/command.go could
// never have caught it, but is within tui.Suggest's edit-distance threshold of
// exactly one real name.
func TestAnUnknownCommandCloseToARealOneGetsASuggestion(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	_, err := d.Run(context.Background(), "/qiot")
	if err == nil {
		t.Fatal("a typo was accepted as a command")
	}

	want := "unknown command /qiot (did you mean /quit?)"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

// TestAnUnknownCommandTooFarFromAnythingGetsNoSuggestion covers the other half of the
// same feature: a name that is not close enough to any real one, by the threshold
// internal/tui/suggest.go settles on, is reported exactly the way it always was, with
// nothing appended. A suggestion offered on every unknown command, whether or not it
// means anything, is noise dressed as help.
func TestAnUnknownCommandTooFarFromAnythingGetsNoSuggestion(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	_, err := d.Run(context.Background(), "/nonesuch")
	if err == nil {
		t.Fatal("an unknown command was accepted")
	}

	want := "unknown command /nonesuch"
	if err.Error() != want {
		t.Errorf("got %q, want %q (no suggestion appended)", err.Error(), want)
	}
}

// TestDidYouMeanJoinsATieWithOr checks the sentence didYouMean builds for a tie
// directly, independent of whether any single typo in the current table happens to
// produce one. This is the "list up to a few names rather than just the first found"
// decision from the PR: when tui.Suggest ties, every tied name is shown, joined as a
// reader would join a short list in a sentence, so the hint reads naturally rather
// than as a bare comma-separated dump.
func TestDidYouMeanJoinsATieWithOr(t *testing.T) {
	cases := []struct {
		guesses []string
		want    string
	}{
		{nil, ""},
		{[]string{"quit"}, " (did you mean /quit?)"},
		{[]string{"color", "close"}, " (did you mean /color or /close?)"},
		{[]string{"close", "color", "copy"}, " (did you mean /close, /color or /copy?)"},
	}

	for _, c := range cases {
		got := joinDidYouMean(c.guesses)
		if got != c.want {
			t.Errorf("joinDidYouMean(%v) = %q, want %q", c.guesses, got, c.want)
		}
	}
}

// TestTheTableButUnbuiltMessageCarriesNoSuggestion covers the other refusal Run
// gives, the one for a name that IS in internal/tui/command.go's table but has no
// handler yet. That message is deliberately distinct from "unknown command" and
// should stay untouched by this feature: a reader who typed a real, listed command
// correctly does not need to be told to try something else.
func TestTheTableButUnbuiltMessageCarriesNoSuggestion(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})

	_, err := d.Run(context.Background(), "/compact")
	if err == nil {
		t.Fatal("a command this build does not run was accepted")
	}

	want := "/compact is in the table but this build does not run it yet"
	if err.Error() != want {
		t.Errorf("got %q, want %q (no suggestion appended)", err.Error(), want)
	}
}

// TestAWriteIsHeldAndNotApplied covers the rule the whole confirmation exists for:
// fetching and showing is one call, and writing is a second one the reader asks for.
func TestAWriteIsHeldAndNotApplied(t *testing.T) {
	var writes atomic.Int64
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPatch, http.MethodDelete:
				writes.Add(1)
				w.Write([]byte(`{"success":true,"result":{"id":"rec2"}}`))
			default:
				w.Write([]byte(zoneBody))
			}
		})

	out, err := d.Run(context.Background(),
		"/cloudflare dns edit --zone example.com --name app.example.com --content 203.0.113.42")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if writes.Load() != 0 {
		t.Fatalf("the write went through before the reader confirmed it")
	}
	for _, want := range []string{
		"-  content: 198.51.100.7",
		"+  content: 203.0.113.42",
		"/cloudflare confirm",
		"   ttl: 300",
	} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the reply does not carry %q:\n%s", want, out.Text)
		}
	}
}

// TestConfirmAppliesExactlyWhatWasShown is the property that makes holding worth
// anything: what is sent is the record the diff was made from.
func TestConfirmAppliesExactlyWhatWasShown(t *testing.T) {
	var sent string
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPatch {
				b := make([]byte, r.ContentLength)
				r.Body.Read(b)
				sent = string(b)
				w.Write([]byte(`{"success":true,"result":{"id":"rec1","name":"app.example.com","content":"203.0.113.42"}}`))
				return
			}
			w.Write([]byte(zoneBody))
		})

	if _, err := d.Run(context.Background(),
		"/cloudflare dns edit --zone example.com --name app.example.com --content 203.0.113.42"); err != nil {
		t.Fatalf("the proposal: %v", err)
	}

	out, err := d.Run(context.Background(), "/cloudflare confirm")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !strings.Contains(out.Text, "changed") {
		t.Errorf("the confirmation did not report the change: %q", out.Text)
	}
	if !strings.Contains(sent, `"content":"203.0.113.42"`) {
		t.Errorf("what was sent was not what was shown: %s", sent)
	}
	if !strings.Contains(sent, `"ttl":300`) {
		t.Errorf("the TTL was dropped rather than carried across: %s", sent)
	}
}

// TestConfirmWithNothingHeldIsAResult covers the case where a reader types the
// confirmation on its own.
func TestConfirmWithNothingHeldIsAResult(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {})

	_, err := d.Run(context.Background(), "/cloudflare confirm")
	if err == nil {
		t.Fatal("a confirmation with nothing held was accepted")
	}
	if !strings.Contains(err.Error(), "nothing to confirm") {
		t.Errorf("the refusal is %q, want it to say there is nothing held", err)
	}
}

// TestAProposalIsConsumedByItsConfirmation covers the second confirmation, which
// would otherwise apply a change the reader has already applied.
func TestAProposalIsConsumedByItsConfirmation(t *testing.T) {
	applied := 0
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPatch {
				applied++
				w.Write([]byte(`{"success":true,"result":{"id":"rec1","name":"app.example.com","content":"203.0.113.42"}}`))
				return
			}
			w.Write([]byte(zoneBody))
		})

	if _, err := d.Run(context.Background(),
		"/cloudflare dns edit --zone example.com --name app.example.com --content 203.0.113.42"); err != nil {
		t.Fatalf("the proposal: %v", err)
	}
	if _, err := d.Run(context.Background(), "/cloudflare confirm"); err != nil {
		t.Fatalf("the first confirm: %v", err)
	}
	if _, err := d.Run(context.Background(), "/cloudflare confirm"); err == nil {
		t.Error("the second confirm applied the change again")
	}
	if applied != 1 {
		t.Errorf("the change was applied %d times, want 1", applied)
	}
}

// TestAProposalThatChangesNothingIsNotHeld covers the case where a reader typed an
// edit that says what is already there.
func TestAProposalThatChangesNothingIsNotHeld(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(zoneBody))
		})

	out, err := d.Run(context.Background(),
		"/cloudflare dns edit --zone example.com --name app.example.com --content 198.51.100.7")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "nothing would change") {
		t.Errorf("the reply is %q, want it to say nothing would change", out.Text)
	}
	if d.pending != nil {
		t.Error("a change that would change nothing was held for confirmation")
	}
}

// TestACreationWhereTheRecordIsThereIsNotHeld covers the duplicate.
func TestACreationWhereTheRecordIsThereIsNotHeld(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(zoneBody))
		})

	out, err := d.Run(context.Background(),
		"/cloudflare dns add --zone example.com --name app.example.com --type A --content 203.0.113.42")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "nothing to add") {
		t.Errorf("the reply is %q, want it to say there is nothing to add", out.Text)
	}
	if d.pending != nil {
		t.Error("a duplicate creation was held for confirmation")
	}
}

// TestARemovalOfNothingIsNotHeld covers the mirror case.
func TestARemovalOfNothingIsNotHeld(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(emptyZone))
		})

	out, err := d.Run(context.Background(),
		"/cloudflare dns delete --zone example.com --name app.example.com")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "nothing to delete") {
		t.Errorf("the reply is %q, want it to say there is nothing to delete", out.Text)
	}
	if d.pending != nil {
		t.Error("a deletion of nothing was held for confirmation")
	}
}

// TestASecondProposalReplacesTheFirst covers the rule that a reader changing their
// mind does not get offered a choice.
func TestASecondProposalReplacesTheFirst(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPatch {
				w.Write([]byte(`{"success":true,"result":{"id":"rec1","name":"app.example.com","content":"203.0.113.42"}}`))
				return
			}
			w.Write([]byte(zoneBody))
		})

	if _, err := d.Run(context.Background(),
		"/cloudflare dns edit --zone example.com --name app.example.com --content 203.0.113.42"); err != nil {
		t.Fatalf("the first proposal: %v", err)
	}
	if _, err := d.Run(context.Background(),
		"/cloudflare dns edit --zone example.com --name app.example.com --content 192.0.2.1"); err != nil {
		t.Fatalf("the second proposal: %v", err)
	}

	if _, err := d.Run(context.Background(), "/cloudflare confirm"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if got := d.pending; got != nil {
		t.Errorf("a proposal is still held after a confirmation: %+v", got)
	}
}

// TestNoProviderMakesNoRequest covers the first-run state, and it is the test the
// old one got wrong.
//
// The old test asserted that a call with no key reported an error naming cloudflare,
// and passed for the wrong reason: its fixture returned an empty body, so the call was
// made, the body failed to decode, and the decode failure happened to contain the word
// cloudflare. Give that fixture a well-formed body and it would still have passed
// with the request going out and no credential on it.
//
// So this one counts the requests. A provider that is not set up makes none at all,
// which is the whole of the rule: there is nothing to authenticate with, and a request
// without a key is one a third party records as a failed attempt against the reader's
// address.
func TestNoProviderMakesNoRequest(t *testing.T) {
	var requests atomic.Int64
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte(zoneBody))
	})

	out, err := d.Run(context.Background(), "/cloudflare dns list --zone example.com")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := requests.Load(); got != 0 {
		t.Errorf("%d requests were made with no provider set up, want 0", got)
	}
	if out.Ask == "" && out.Text == "" {
		t.Error("the reader was told nothing: a command that produces no result and " +
			"no reply is one they cannot act on")
	}
}

// TestNoProviderAsksTheModel covers the preference for a model over carried text,
// since the configuration syntax can change and this binary cannot follow it.
func TestNoProviderAsksTheModel(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})
	d.canAsk = func() bool { return true }

	out, err := d.Run(context.Background(), "/cloudflare dns list --zone example.com")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Ask == "" {
		t.Fatal("no question was sent to the model")
	}

	// The question is written into the log as a row before the answer arrives, so a
	// question phrased for a model has to be worth a reader reading. What it has to
	// say is the fact it is certain of and what it wants asked, since the detail is
	// exactly what it is asking the model for.
	for _, want := range []string{"cloudflare", "configuration file", "Tell me"} {
		if !strings.Contains(out.Ask, want) {
			t.Errorf("the question does not mention %q, so a reader reading it learns "+
				"nothing:\n%s", want, out.Ask)
		}
	}
}

// TestNoProviderFallsBackWhenThereIsNoModel covers the case the fallback exists for.
// A reader with no credential and no model cannot be answered by a model, and the
// command must still produce a result rather than nothing.
func TestNoProviderFallsBackWhenThereIsNoModel(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})
	d.canAsk = func() bool { return false }

	out, err := d.Run(context.Background(), "/cloudflare dns list --zone example.com")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Ask != "" {
		t.Error("a question was sent to a model that cannot answer")
	}
	if !strings.Contains(out.Text, "cloudflare") {
		t.Errorf("the fallback does not name the block:\n%s", out.Text)
	}
}

// credentialShaped is what a real key looks like in this client's own words: the
// OpenRouter prefix followed by something, or a Cloudflare token of the length one
// carries.
//
// The test is against the shape and not against one fixture, since a check for one
// literal string is a check a writer satisfies by changing the literal.
var credentialShaped = regexp.MustCompile(`sk-or-v1-[A-Za-z0-9_-]{8,}|cf-[A-Za-z0-9_-]{16,}`)

// TestTheGuidanceCarriesNoCredential is the rule the whole shape rests on. A string in
// a binary that looks like a key is a string a reader might paste somewhere, and a
// placeholder cannot be.
func TestTheGuidanceCarriesNoCredential(t *testing.T) {
	for _, s := range []string{cloudflareAskFallback, cloudflareAsk} {
		if m := credentialShaped.FindString(s); m != "" {
			t.Errorf("the guidance carries a credential-shaped string %q", m)
		}
	}

	// The fallback shows the shape, so it has to show it with something that cannot
	// be a key in it. An ellipsis after a prefix is not a key, but a reader pasting
	// it is pasting nothing, which is the point.
	if !strings.Contains(cloudflareAskFallback, "YOUR-CLOUDFLARE-API-KEY") {
		t.Error("the fallback does not show the shape with a placeholder key")
	}
}

// TestTheCredentialIsNeverInTheOutput covers the rule every row depends on: a log
// row is a thing a reader selects and pastes somewhere else.
func TestTheCredentialIsNeverInTheOutput(t *testing.T) {
	const key = "cf-live-the-credential"

	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"`+key+`"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"success":false,"errors":[{"code":6003,"message":"bad key ` + key + `"}]}`))
		})

	out, err := d.Run(context.Background(), "/cloudflare dns list --zone example.com")
	if err != nil && strings.Contains(err.Error(), key) {
		t.Errorf("the credential is in the error: %q", err)
	}
	if strings.Contains(out.Text, key) {
		t.Errorf("the credential is in the output: %q", out.Text)
	}
}

// TestARefusedCallIsAResult covers the rule that nothing produces nothing: a 400
// from the endpoint is text the caller shows, not an empty answer.
func TestARefusedCallIsAResult(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"success":false,"errors":[{"code":1004,"message":"invalid DNS name"}]}`))
		})

	if _, err := d.Run(context.Background(), "/cloudflare dns list --zone example.com"); err == nil {
		t.Fatal("a 400 was reported as a success")
	} else if !strings.Contains(err.Error(), "invalid DNS name") {
		t.Errorf("the refusal is %q, want it to carry what the endpoint said", err)
	}
}

// TestAnEmptyZoneIsReportedAsEmpty covers the case where there is nothing wrong.
func TestAnEmptyZoneIsReportedAsEmpty(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(emptyZone))
		})

	out, err := d.Run(context.Background(), "/cloudflare dns list --zone example.com")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "no records") {
		t.Errorf("the reply is %q, want it to say the zone has none", out.Text)
	}
}

// TestTheUsageNamesWhatIsBuilt covers the case a reader reaches by typing the
// command on its own.
func TestTheUsageNamesWhatIsBuilt(t *testing.T) {
	d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
		func(w http.ResponseWriter, r *http.Request) {})

	out, err := d.Run(context.Background(), "/cloudflare")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"dns list", "dns add", "/cloudflare confirm"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the usage does not carry %q:\n%s", want, out.Text)
		}
	}
	if !strings.Contains(out.Text, "not built yet") {
		t.Errorf("the usage does not say which of the five are built:\n%s", out.Text)
	}
}

// TestAnUnbuiltSubcommandIsRefusedByName covers the four the design names and this
// build does not have.
func TestAnUnbuiltSubcommandIsRefusedByName(t *testing.T) {
	for _, sub := range []string{"zone", "cache", "page-rules", "account"} {
		d := over(t, `{"api_key":"k","cloudflare":{"api_key":"cf-key"}}`,
			func(w http.ResponseWriter, r *http.Request) {})

		_, err := d.Run(context.Background(), "/cloudflare "+sub)
		if err == nil {
			t.Errorf("/cloudflare %s was accepted", sub)
			continue
		}
		if !strings.Contains(err.Error(), sub) {
			t.Errorf("the refusal for %s is %q, want it to name it", sub, err)
		}
	}
}

// TestLevelWithNoArgumentReportsNoPreset covers the reader who has never typed
// /level, the way /model with no argument reports before any model is chosen.
func TestLevelWithNoArgumentReportsNoPreset(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})
	d.withSession(tui.New(tui.Options{}))

	out, err := d.Run(context.Background(), "/level")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "no level preset is set") {
		t.Errorf("the report is %q, want it to say none is set", out.Text)
	}
	for _, want := range tui.PresetNames() {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the report does not list the preset %q:\n%s", want, out.Text)
		}
	}
}

// TestLevelSetsThePresetAndIsReported covers the write and the read-back, since a
// preset that could not be reported would be one the reader cannot confirm took.
func TestLevelSetsThePresetAndIsReported(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})
	s := tui.New(tui.Options{})
	d.withSession(s)

	out, err := d.Run(context.Background(), "/level direct")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "direct") {
		t.Errorf("the reply is %q, want it to name the preset", out.Text)
	}
	if got := s.Preset(); got != "direct" {
		t.Errorf("s.Preset() is %q, want %q", got, "direct")
	}
	if got := s.Options().Verbosity; got != 1 {
		t.Errorf("Verbosity is %d, want the direct preset's 1", got)
	}

	out, err = d.Run(context.Background(), "/level")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.Text, "direct") {
		t.Errorf("/level with no argument did not report the preset: %q", out.Text)
	}
}

// TestLevelWithAnUnknownNameIsRefusedByName covers the typo, since a reader who
// mistyped a preset should be told what exists rather than left to guess.
func TestLevelWithAnUnknownNameIsRefusedByName(t *testing.T) {
	d := over(t, `{"api_key":"k"}`, func(w http.ResponseWriter, r *http.Request) {})
	d.withSession(tui.New(tui.Options{}))

	_, err := d.Run(context.Background(), "/level nosuch")
	if err == nil {
		t.Fatal("an unknown preset was accepted")
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("the refusal is %q, want it to name what was typed", err)
	}
}

// TestLevelStyleReachesTheSystemMessage covers the point of PresetStyle: the
// instruction a preset carries has to be something a turn actually sends, not only
// a label the status bar shows.
func TestLevelStyleReachesTheSystemMessage(t *testing.T) {
	s := tui.New(tui.Options{})
	d := newDispatcherFor(config.Config{})
	d.withSession(s)

	if _, err := d.Run(context.Background(), "/level direct"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msg := capabilities(s, nil, nil)
	if !strings.Contains(msg, "Be direct") {
		t.Errorf("the system message does not carry the preset's style:\n%s", msg)
	}
}

// TestShortIDKeepsWhatARowCanShow covers the trim on an identifier, since a 32 digit
// row is a row a reader scrolls past.
func TestShortIDKeepsWhatARowCanShow(t *testing.T) {
	if got := shortID("0123456789abcdef0123456789abcdef"); got != "0123456789ab" {
		t.Errorf("shortID gave %q", got)
	}
	if got := shortID("short"); got != "short" {
		t.Errorf("shortID trimmed an identifier that was already short: %q", got)
	}
}

// loadBody writes a configuration at 0600 and reads it back through Parse.
//
// The file is real rather than a struct on purpose: the block reader is one of the
// things under test, and building a Config by hand would bypass it. The search order
// is pointed at a temporary home so a test cannot reach the reader's own file.
func loadBody(t *testing.T, body string) config.Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "orcli.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the configuration: %v", err)
	}

	cfg, err := config.Parse(data, path)
	if err != nil {
		t.Fatalf("parse the configuration: %v", err)
	}
	return cfg
}
