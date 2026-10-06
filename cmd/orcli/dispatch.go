package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/glenjbarber/orcli/internal/cloudflare"
	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// handler runs one command and returns what the reader should be shown.
//
// It takes the dispatcher rather than reaching for it through a package variable, so
// two dispatchers in one test run cannot reach each other's held proposal.
type handler func(ctx context.Context, d *dispatcher, args string) (tui.Result, error)

// dispatcher runs the commands and holds what one command leaves for the next.
//
// It lives in main rather than in internal/tui for the reason the design record gives:
// a command here needs a credential the interface has no use for, and a session
// holding a provider key it never reads would be a credential kept for the convenience
// of a caller that can reach the file itself. AGENTS.md holds that main is where the
// direction changes, and this is that.
//
// The held proposal is a field here rather than on the session for the same reason. A
// session is the log, the levels, the workers and the four states; a proposed DNS
// record is none of those.
type dispatcher struct {
	// cfg is the configuration as startup read it, held as a configuration rather
	// than as a credential so the block is read where it is needed and so a test can
	// build one over a file it wrote.
	cfg config.Config

	// session is the interface's own session, so `/model` can change the model the
	// next turn is sent with rather than only the one written to the file.
	//
	// It is a pointer rather than a value because the session is the thing the
	// interface is drawing and a command changing what it is answering with has to
	// change that, not a copy of it.
	session *tui.Session

	// commands is the table, built once at construction.
	commands map[string]handler

	// pending is the change /cloudflare is holding between a proposal and
	// /cloudflare confirm, together with the state it was made against.
	pending *cloudflare.Proposal
	before  cloudflare.Record
	existed bool

	// client is the API client, built on first use, and newClient is what builds it.
	// It is a field rather than a package variable so two dispatchers in one test run
	// cannot reach each other's transport.
	client    *cloudflare.APIClient
	newClient func(key string) *cloudflare.APIClient

	// canAsk reports whether a model can be asked at all, which decides whether
	// /cloudflare sends its guidance to the model or falls back to its own text.
	canAsk func() bool
}

// newDispatcherFor builds a dispatcher over a configuration.
func newDispatcherFor(cfg config.Config) *dispatcher {
	d := &dispatcher{cfg: cfg, newClient: cloudflare.New}
	d.canAsk = func() bool { return false }
	d.commands = map[string]handler{
		"cloudflare": func(ctx context.Context, d *dispatcher, args string) (tui.Result, error) {
			return d.cloudflare(ctx, args)
		},
		"model": func(ctx context.Context, d *dispatcher, args string) (tui.Result, error) {
			return d.model(args)
		},
		"test": func(ctx context.Context, d *dispatcher, args string) (tui.Result, error) {
			return d.test(args)
		},
		"quit": func(ctx context.Context, d *dispatcher, args string) (tui.Result, error) {
			return tui.Result{Quit: true}, nil
		},
	}
	return d
}

// withSession hands the dispatcher the interface's own session.
//
// It is a separate call rather than a field set at construction, since the dispatcher
// is built before the session is handed to the interface and the two are wired
// together in openInterface. A dispatcher with no session can still run the commands
// that do not touch one, which is what a test over /cloudflare does.
func (d *dispatcher) withSession(s *tui.Session) { d.session = s }

// model is the handler for `/model`.
//
// It chooses a model, goes back to the one before it, and reports both when asked
// with no argument. See modelHandler for the four cases and why they are told apart
// before anything is written.
func (d *dispatcher) model(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/model needs an open interface")
	}
	return modelHandler(d.session, args)
}

// Run executes a typed line.
//
// A command the dispatcher does not have is reported by name, and the two cases are
// told apart. The table in internal/tui lists thirty-nine names and this build
// implements four, so a reader who typed /copy is told the name is in the table and
// this build does not run it, rather than being told there is no such command: those
// are different faults and a reader told the second goes looking for a typo.
//
// A line that is not a command is not this function's business. The loop sends a
// question to the model and a command here, and a dispatcher that also answered
// questions would be two things deciding what a line means.
func (d *dispatcher) Run(ctx context.Context, line string) (tui.Result, error) {
	name, args, ok := tui.IsCommand(line)
	if !ok {
		return tui.Result{}, nil
	}

	h, known := d.commands[name]
	if !known {
		if _, listed := tui.Lookup(name); listed {
			return tui.Result{}, fmt.Errorf("/%s is in the table but this build does not run it yet", name)
		}
		return tui.Result{}, fmt.Errorf("unknown command /%s", name)
	}

	return h(ctx, d, args)
}

// test is the handler for /test.
//
// It lists a directory and hands the listing back as the text, which the loop writes
// where a reply from the model is written, so the log fills with rows the reader chose
// and the frame can be watched drawing them. It is a development command: it reaches no
// network, asks no model, and reads nothing outside the directory it is given.
//
// The listing is one result rather than a row per entry. The loop writes a result as a
// single notice, and a handler that wanted a row per entry would have to reach past the
// result and into the log, which is the one thing the loop owns. So a listing is one
// string carrying newlines, and the row it becomes is folded to the width of the
// terminal. That is worth knowing before the test rather than after it.
//
// A directory is written with a trailing slash, since a name cannot say what it is and
// the listing is read on a terminal rather than parsed.
func (d *dispatcher) test(args string) (tui.Result, error) {
	dir := strings.TrimSpace(args)
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/test: %w", err)
	}
	if len(entries) == 0 {
		return tui.Result{Text: fmt.Sprintf("%s is empty", dir)}, nil
	}

	// The listing is sorted by name rather than left in the order the filesystem hands
	// the entries over, since a reader comparing two runs of the same command is
	// comparing what is on disk and an unsorted listing changes with the order the
	// directory was written in.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte('\n')
		}
		if entries[i].IsDir() {
			b.WriteString(n + "/")
			continue
		}
		b.WriteString(n)
	}
	return tui.Result{Text: b.String()}, nil
}

// cloudflareKey returns the credential the Cloudflare commands make calls with.
//
// It is read from the block rather than held as a field, so the configuration is the one
// place the key lives and there is no second copy to fall out of step with the file.
func (d *dispatcher) cloudflareKey() (string, error) {
	return d.cfg.CloudflareAPIKey()
}

// cloudflareReady reports whether a Cloudflare call could be made right now, without
// building the client. It is the check adr-0000042's capability message names the
// connector by - the credential is fixed for the session today, set once at
// configuration load with no command that changes it, but this checks fresh anyway
// rather than caching a bool, so a later writer never has to remember to invalidate one.
func (d *dispatcher) cloudflareReady() bool {
	key, err := d.cloudflareKey()
	return err == nil && key != ""
}

// cloudflareClient returns the API client, building it on first use.
//
// The client is built rather than handed in, since a handler that took one would have
// every caller construct it, and a caller that forgot the credential filter would be a
// caller whose diagnostics carry the key.
//
// An empty key is refused here, before anything is built and before any request is made.
// The check has to be at this level because the caller has to know that no provider is
// set up in order to ask the reader, and a refusal raised from deeper in would arrive
// too late to be answered differently.
func (d *dispatcher) cloudflareClient() (*cloudflare.APIClient, error) {
	key, err := d.cloudflareKey()
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, cloudflare.ErrNoCredential
	}

	if d.client != nil {
		return d.client, nil
	}
	d.client = d.newClient(key)
	return d.client, nil
}

// cloudflare is the handler for /cloudflare.
//
// Every outcome is a result: an unknown sub-command, a missing flag, and an API refusal
// are all text the caller shows, which is what AGENTS.md holds: a call always produces
// a result.
func (d *dispatcher) cloudflare(ctx context.Context, args string) (tui.Result, error) {
	// The provider is checked before the sub-command is even read, since a reader who
	// has not set one up is not helped by being told which of five sub-commands this
	// build has.
	if _, err := d.cloudflareKey(); err != nil {
		return tui.Result{}, err
	} else if key, _ := d.cloudflareKey(); key == "" {
		return d.notSetUp(), nil
	}

	if strings.TrimSpace(args) == "" {
		return tui.Result{Text: d.cloudflareUsage()}, nil
	}

	action, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	switch action {
	case "confirm":
		return d.cloudflareConfirm(ctx)
	case "dns":
		return d.cloudflareDNS(ctx, rest)
	case "zone", "cache", "page-rules", "account":
		return tui.Result{}, fmt.Errorf("/cloudflare %s is in the design and not built yet", action)
	default:
		return tui.Result{}, fmt.Errorf("/cloudflare %s is not a sub-command", action)
	}
}

// notSetUp is what /cloudflare does when no provider is configured.
//
// No API call is made at all. Not a call that fails and not a call with an empty
// credential: no call, because there is nothing to authenticate with and a request
// without it is a request a third party records as a failed attempt against the
// reader's address.
//
// The guidance goes to the model rather than being carried as a string in the binary,
// since the configuration syntax can change and the binary does not. The client keeps
// the one fact it is certain of, which is that the provider is not set up, and asks the
// model for the rest: the shape of the block, where the file is, what mode it carries,
// and an example with a sample key.
//
// A reader with no model and no credential cannot be answered by a model, so the
// fallback is this client's own short text. That text is the thing that drifts, which
// is why it is the fallback and not the default.
func (d *dispatcher) notSetUp() tui.Result {
	if d.canAsk != nil && d.canAsk() {
		return tui.Result{Ask: cloudflareAsk}
	}
	return tui.Result{Text: cloudflareAskFallback}
}

// cloudflareAsk is the question sent to the model when the provider is not set up.
//
// The wording is what the reader sees, since Begin writes the question into the log as
// a row before the answer arrives: a question phrased for a model but read by a person
// has to be worth reading.
const cloudflareAsk = `The Cloudflare provider is not set up in the configuration file, so /cloudflare cannot run. Tell me how to add one: the exact JSON shape of the cloudflare block, where the configuration file is found and what mode it must carry, and an example with a sample API key.`

// cloudflareAskFallback is what the client says when there is no model to ask.
//
// It is deliberately short and it names only what this client is certain of. The
// example is a placeholder and never a real key: a string in a binary that looks like
// a credential is a string a reader might paste somewhere, and a placeholder cannot be.
const cloudflareAskFallback = `The Cloudflare provider is not set up, so /cloudflare cannot run.

Put a cloudflare object in your configuration file alongside "api_key":

  {
    "api_key": "sk-or-v1-...",
    "cloudflare": {
      "api_key": "YOUR-CLOUDFLARE-API-KEY"
    }
  }

The file is ~/.orcli.json or ~/.config/orcli/orcli.json and must be mode 0600.`

// cloudflareUsage is what /cloudflare alone reports.
//
// It lists what this build does rather than every sub-command in the design, since four
// of the five are not built and a usage listing them would be a usage a reader types
// and is then refused.
func (d *dispatcher) cloudflareUsage() string {
	return strings.Join([]string{
		"/cloudflare dns list --zone NAME",
		"/cloudflare dns add --zone NAME --name NAME --type TYPE --content VALUE [--ttl N]",
		"/cloudflare dns edit --zone NAME --name NAME --content VALUE [--ttl N]",
		"/cloudflare dns delete --zone NAME --name NAME",
		"/cloudflare confirm",
		"",
		"zone, cache, page-rules and account are in the design and not built yet.",
	}, "\n")
}

// dnsFlags are the flags the dns actions take.
//
// One table for all four, since ParseDNS refuses a flag that is not in it and a
// per-action table would let an action accept a flag it ignores, which is a reader
// typing something that silently does nothing.
var dnsFlags = map[string]bool{
	"zone":      true,
	"name":      true,
	"type":      true,
	"content":   true,
	"ttl":       true,
	"record-id": true,
	"proxied":   true,
}

// cloudflareDNS runs one dns action.
func (d *dispatcher) cloudflareDNS(ctx context.Context, args string) (tui.Result, error) {
	client, err := d.cloudflareClient()
	if err != nil {
		return tui.Result{}, err
	}

	verb, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	parsed, err := cloudflare.ParseDNS(verb, strings.TrimSpace(rest), dnsFlags)
	if err != nil {
		return tui.Result{}, err
	}

	zone, err := parsed.Require("zone")
	if err != nil {
		return tui.Result{}, err
	}

	switch parsed.Verb {
	case "list":
		records, err := client.ListRecords(zone)
		if err != nil {
			return tui.Result{}, err
		}
		return tui.Result{Text: renderRecords(zone, records)}, nil

	case "add", "edit", "delete":
		text, err := d.propose(client, parsed, zone)
		if err != nil {
			return tui.Result{}, err
		}
		return tui.Result{Text: text}, nil

	default:
		return tui.Result{}, fmt.Errorf("/cloudflare dns %s is not a sub-command", parsed.Verb)
	}
}

// propose holds a change for confirmation, and is shared by the three writes.
//
// The three differ in what the proposed record is made from; they agree in everything
// else. Each fetches the current state, each asks the proposal whether it changes
// anything, and each holds what was shown. One function rather than three, since three
// copies of the fetch-then-diff-then-hold sequence is three places for a write to skip
// the confirmation.
func (d *dispatcher) propose(client *cloudflare.APIClient, a cloudflare.Args, zone string) (string, error) {
	name, err := a.Require("name")
	if err != nil {
		return "", err
	}

	before, existed, err := client.FindRecord(zone, name)
	if err != nil {
		return "", err
	}

	// A creation where a record of that name is already there, and a change or a
	// removal where it is not, are what a reader gets when the zone and the name name
	// different things. Both are reported rather than diffed against nothing, since a
	// diff against no record reads as a real change and is not one.
	switch {
	case a.Verb == "add" && existed:
		return fmt.Sprintf("%s already has a record at %s, so there is nothing to add",
			zone, name), nil
	case a.Verb != "add" && !existed:
		return fmt.Sprintf("%s has no record at %s, so there is nothing to %s",
			zone, name, a.Verb), nil
	}

	// The proposed record starts from the current one where there is one, so a flag
	// the reader did not name leaves the value the endpoint already holds rather than
	// zeroing it. An addition has nothing to start from and is built from the flags.
	proposed := cloudflare.Record{}
	if existed {
		proposed = before
	}
	proposed.Name = name
	if err := a.Apply(&proposed); err != nil {
		return "", err
	}

	// A creation is the one case with no prior record to take a type from, so the type
	// is required rather than defaulted: a record with no type is not a record.
	if a.Verb == "add" {
		recordType, err := a.Require("type")
		if err != nil {
			return "", err
		}
		proposed.Type = recordType
	}
	// The content is required for both a creation and a change, since a record with no
	// content is a record that resolves nowhere and the diff would show a reader a
	// change they cannot read.
	if _, err := a.Require("content"); err != nil {
		return "", err
	}

	p := cloudflare.Proposal{
		Action:   a.Action(),
		Zone:     zone,
		RecordID: before.ID,
		Name:     name,
		Proposed: proposed,
	}

	if !p.Change(before, existed) {
		return fmt.Sprintf("the record at %s in %s already says that, so nothing would change",
			name, zone), nil
	}

	return d.hold(p, before, existed), nil
}

// hold records a proposal and returns what the reader is shown.
//
// The proposal replaces anything already held, which is what the design settles: a
// second /cloudflare call before the confirmation is a reader changing their mind about
// what they want changed, and holding the first alongside it would offer them a choice
// they did not ask for.
func (d *dispatcher) hold(p cloudflare.Proposal, before cloudflare.Record, existed bool) string {
	d.pending = &p
	d.before = before
	d.existed = existed

	return strings.Join([]string{
		p.Diff(before, existed),
		"",
		"  /cloudflare confirm    apply this",
		"  anything else          cancel",
	}, "\n")
}

// cloudflareConfirm applies the held proposal, and reports having nothing to do.
//
// The proposal is consumed before the call is made rather than after it succeeds, so a
// failed confirmation does not leave a change held that the reader has already tried and
// failed to apply. What is applied is the record the proposal carries, which is the one
// the diff was made from, so nothing is rebuilt from arguments typed a second time.
func (d *dispatcher) cloudflareConfirm(ctx context.Context) (tui.Result, error) {
	if d.pending == nil {
		return tui.Result{}, fmt.Errorf("there is nothing to confirm: /cloudflare proposes a change first")
	}

	client, err := d.cloudflareClient()
	if err != nil {
		return tui.Result{}, err
	}

	p := *d.pending
	d.pending = nil

	switch p.Action {
	case cloudflare.AddRecord:
		record, err := client.CreateRecord(p.Zone, p.Proposed)
		if err != nil {
			return tui.Result{}, err
		}
		return tui.Result{Text: fmt.Sprintf("added %s %s in %s as %s",
			record.Type, record.Name, p.Zone, shortID(record.ID))}, nil

	case cloudflare.EditRecord:
		record, err := client.UpdateRecord(p.Zone, p.RecordID, p.Proposed)
		if err != nil {
			return tui.Result{}, err
		}
		return tui.Result{Text: fmt.Sprintf("changed %s in %s to %s",
			record.Name, p.Zone, record.Content)}, nil

	case cloudflare.DeleteRecord:
		if err := client.DeleteRecord(p.Zone, p.RecordID); err != nil {
			return tui.Result{}, err
		}
		return tui.Result{Text: fmt.Sprintf("removed %s from %s", p.Name, p.Zone)}, nil

	default:
		return tui.Result{}, fmt.Errorf("the held change is not one this build knows")
	}
}

// renderRecords lists a zone's records, one per line.
//
// The fields are the ones the reader confirms a change against, in the same order the
// diff uses, so a list and a diff of the same record read alike. The API's own JSON
// would be its answer rather than an answer, and the design asks for one a reader can
// scan.
func renderRecords(zone string, records []cloudflare.Record) string {
	if len(records) == 0 {
		return fmt.Sprintf("%s has no records", zone)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d records\n", zone, len(records))
	for _, r := range records {
		fmt.Fprintf(&b, "  %s %s -> %s  ttl %d", r.Type, r.Name, r.Content, r.TTL)
		if r.Proxied {
			b.WriteString("  proxied")
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// shortID trims an identifier to what a reader can compare.
//
// A Cloudflare identifier is 32 hex digits and a row carrying all of it is a row a reader
// scrolls past. The head is enough to tell two records apart, and the whole of it is in
// the copy.
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
