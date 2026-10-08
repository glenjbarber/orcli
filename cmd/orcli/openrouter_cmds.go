package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tui"
)

// catalogClient is the two methods /key, /models, /freemodels and /attribute need
// from the configured provider's own client - *openrouter.Client when the session
// is configured for OpenRouter, *groq.Client when it is configured for Groq (see
// isGroqProvider in provider.go and newDispatcherFor's own construction of
// newORClient in dispatch.go).
//
// It exists for the reason chatClient does: a test can hand the dispatcher a fake
// that answers in memory, since neither client's base URL can be pointed at a test
// server from outside its own package (see chatClient's own doc comment).
// *openrouter.Client and *groq.Client both already satisfy this with no change to
// either: internal/groq.Client.Models returns the same openrouter.ModelInfo
// openrouter.Client.Models does, for the reason internal/groq's own package doc
// gives, and internal/groq.Client.KeyUsage returns the same openrouter.KeyInfo
// alongside a fixed error, since Groq publishes no usage-accounting endpoint for
// this method to read (see internal/groq/key.go).
type catalogClient interface {
	Models(ctx context.Context) ([]openrouter.ModelInfo, error)
	KeyUsage(ctx context.Context) (openrouter.KeyInfo, error)
}

// openrouterReady reports whether a catalog or chat call could be made right now,
// without building the client - against whichever provider the configuration
// names, OpenRouter or Groq alike, since this build holds exactly one credential
// per session either way (see internal/config.Config.APIKey's own doc comment).
//
// It mirrors cloudflareReady for the credential this program runs on rather than
// the optional Cloudflare one: the key is read fresh from the block rather than
// cached as a bool, so nothing here has to remember to invalidate a cache the
// reader's configuration was never written to change in the first place.
//
// The name is kept as it was before Groq support existed, for the reason the
// orClient field's own comment in dispatch.go gives: it reads correctly for every
// session configured for OpenRouter, which remains the default and the common
// case, and a provider-neutral rename here would touch nothing a caller outside
// this file can observe.
func (d *dispatcher) openrouterReady() bool {
	return d.cfg.APIKey != ""
}

// catalogClient builds the configured provider's own catalog client, caching it
// the way cloudflareClient caches the Cloudflare one.
//
// The client is built rather than handed in for the same reason: a handler that
// took one would have every caller construct it, and a caller that forgot the
// credential filter would be a caller whose diagnostics carry the key. An empty
// key is refused here, before anything is built, so the refusal is openrouter's
// own ErrNoAPIKey - reused rather than duplicated for a Groq-configured session
// too, since the two providers fail this one local check for the identical reason
// and a second, Groq-spelled error for the same fact would be a second message a
// reader has to learn means the same thing - rather than a request the endpoint
// answers the same way it answers an invalid one.
func (d *dispatcher) catalogClient() (catalogClient, error) {
	if d.cfg.APIKey == "" {
		return nil, openrouter.ErrNoAPIKey
	}
	if d.orClient != nil {
		return d.orClient, nil
	}
	d.orClient = d.newORClient(d.cfg.APIKey)
	return d.orClient, nil
}

// key is the handler for /key.
//
// It reports the usage OpenRouter's own accounting holds against the configured
// credential, not a total this program keeps itself: a total kept here would start
// at zero on every restart and would disagree with the endpoint's own count the
// moment a second client made a request this one never saw. See
// [openrouter.Client.KeyUsage] for why the figure comes from the endpoint rather
// than from arithmetic over requests this session happened to make.
func (d *dispatcher) key(ctx context.Context) (tui.Result, error) {
	client, err := d.catalogClient()
	if err != nil {
		return tui.Result{}, err
	}

	info, err := client.KeyUsage(ctx)
	if err != nil {
		return tui.Result{}, err
	}
	return tui.Result{Text: reportKeyUsage(info)}, nil
}

// reportKeyUsage renders a KeyInfo the way a reader asking "what have I spent"
// wants to read it: the label first, since that is the key being described, then
// what has been spent, then what is left.
//
// A limit of nil is rendered as "no limit set" rather than as a number, on the
// same "absence is a statement" grounds adr-0000042's capability message keeps
// (see capabilities in ask.go): a reader told "$0.00 limit" cannot tell whether
// that is a hard cap of zero or a key that tracks no cap at all, and those are
// different things to be told.
func reportKeyUsage(info openrouter.KeyInfo) string {
	var b strings.Builder
	label := info.Label
	if label == "" {
		label = "-"
	}
	fmt.Fprintf(&b, "key %s: $%.4f spent", label, info.Usage)

	if info.Limit == nil {
		b.WriteString(", no limit set")
	} else {
		fmt.Fprintf(&b, " of a $%.4f limit", *info.Limit)
		if info.LimitRemaining != nil {
			fmt.Fprintf(&b, " ($%.4f remaining)", *info.LimitRemaining)
		}
	}

	if info.IsFreeTier {
		b.WriteString("; restricted to free models")
	}
	return b.String()
}

// models is the handler for /models.
//
// Every call re-fetches the catalog rather than caching it across calls. The
// catalog is the one thing in this cluster that can change out from under a
// session with no command of its own that would tell a reader it had - a model
// can be added, removed, or re-priced on OpenRouter's own schedule - and a session
// that cached it would answer a later /models with a list that was true when the
// session started rather than the one the reader is about to send a request
// against.
func (d *dispatcher) models(ctx context.Context, args string) (tui.Result, error) {
	return d.listModels(ctx, args, false)
}

// freemodels is the handler for /freemodels.
//
// It is [dispatcher.models] with one more filter applied, rather than a separate
// fetch, because the two report the same catalog and a second fetch would be a
// second place for the two to disagree about what the endpoint currently offers.
// What "free" means is [openrouter.ModelInfo.Free]: the endpoint's own pricing
// block reporting zero on both figures, not a guess from the model's name.
func (d *dispatcher) freemodels(ctx context.Context, args string) (tui.Result, error) {
	return d.listModels(ctx, args, true)
}

// listModels is the shared fetch-filter-render sequence /models and /freemodels
// both run.
func (d *dispatcher) listModels(ctx context.Context, args string, freeOnly bool) (tui.Result, error) {
	client, err := d.catalogClient()
	if err != nil {
		return tui.Result{}, err
	}

	all, err := client.Models(ctx)
	if err != nil {
		return tui.Result{}, err
	}

	filter := strings.ToLower(strings.TrimSpace(args))
	matched := make([]openrouter.ModelInfo, 0, len(all))
	for _, m := range all {
		if freeOnly && !m.Free() {
			continue
		}
		if filter != "" &&
			!strings.Contains(strings.ToLower(m.ID), filter) &&
			!strings.Contains(strings.ToLower(m.Name), filter) {
			continue
		}
		matched = append(matched, m)
	}

	// Sorted by identifier rather than left in the order the endpoint answered
	// with, for the reason /test's own directory listing is sorted: a reader
	// typing the same filter twice is comparing two runs of the same command,
	// and an unsorted listing changes with an order this client did not choose.
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

	return tui.Result{Text: renderModels(matched, freeOnly)}, nil
}

// renderModels lists models one per line, the identifier a reader would type
// into /model or /attribute first since that is the value those commands read,
// the display name beside it, and the price last since a free listing's price is
// the same line repeated and a priced listing's is the detail a reader compares.
func renderModels(models []openrouter.ModelInfo, freeOnly bool) string {
	if len(models) == 0 {
		if freeOnly {
			return "no free models match"
		}
		return "no models match"
	}

	var b strings.Builder
	for i, m := range models {
		if i > 0 {
			b.WriteByte('\n')
		}
		if freeOnly {
			fmt.Fprintf(&b, "%s  %s", m.ID, m.Name)
			continue
		}
		fmt.Fprintf(&b, "%s  %s  prompt %s / completion %s per token",
			m.ID, m.Name, orDash(m.Pricing.Prompt), orDash(m.Pricing.Completion))
	}
	return b.String()
}

// orDash renders an empty pricing figure as a dash, on the same grounds
// orNoneModel renders an absent model as one in confirm.go.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// search is the handler for /search.
//
// It is local, not a network call: the log is this session's own record of what
// has already been written, and a reader filtering it is filtering something that
// already arrived rather than asking the endpoint for anything. It runs over
// [tui.Log.Rows] fresh on every call rather than an index built once, since the
// log changes every turn and an index would have to be kept in step with every
// append, delete and restore the log already has its own methods for.
//
// A row matches by a case-insensitive substring of its text, which is the same
// comparison [dispatcher.listModels] applies to the catalog: a reader typing part
// of a word should not have to match the case the model or their own question
// happened to use.
func (d *dispatcher) search(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/search needs an open interface")
	}

	query := strings.TrimSpace(args)
	if query == "" {
		return tui.Result{}, fmt.Errorf("/search needs TEXT to filter the log on")
	}

	needle := strings.ToLower(query)
	rows := d.session.Log().Rows()

	var b strings.Builder
	matches := 0
	for _, row := range rows {
		if !strings.Contains(strings.ToLower(row.Text), needle) {
			continue
		}
		if matches > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(row.Text)
		matches++
	}

	if matches == 0 {
		return tui.Result{Text: fmt.Sprintf("no rows match %q", query)}, nil
	}
	return tui.Result{Text: b.String()}, nil
}

// attribute is the handler for /attribute.
//
// See internal/tui/attribute.go's doc comment for what sets this apart from
// /model: /model accepts any string the reader types and finds out from the next
// request whether the endpoint recognizes it, while /attribute's whole promise is
// that the value has to be one the endpoint actually offers, checked here against
// a fresh [catalogClient.Models] call before anything is set. A reader who typed
// the wrong model once and watched a request be refused would rather be told
// before the request than after it.
//
// Unlike /model, it changes only what this session answers with, through
// [tui.Session.SetModel] - it does not write to the configuration file and does
// not touch last_model. /model's file write and its "last" swap exist for a value
// the reader chose to persist across sessions; /attribute is this session
// confirming the model it is about to use is a real one, which is a narrower
// promise than a write to disk would claim.
func (d *dispatcher) attribute(ctx context.Context, args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/attribute needs an open interface")
	}

	want := strings.TrimSpace(args)
	if want == "" {
		return tui.Result{Text: fmt.Sprintf("the model is %s", orNoneModel(d.session.Options().Model))}, nil
	}

	client, err := d.catalogClient()
	if err != nil {
		return tui.Result{}, err
	}

	models, err := client.Models(ctx)
	if err != nil {
		return tui.Result{}, err
	}

	offered := false
	for _, m := range models {
		if m.ID == want {
			offered = true
			break
		}
	}
	if !offered {
		return tui.Result{}, fmt.Errorf("/attribute %s is not a model the endpoint offers", want)
	}

	if err := d.session.SetModel(want); err != nil {
		return tui.Result{}, err
	}
	return tui.Result{Text: fmt.Sprintf("the model is %s", want)}, nil
}
