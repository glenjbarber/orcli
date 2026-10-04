package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/glenjbarber/orcli/internal/cloudflare"
	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// handler runs one command and returns what the reader should be shown.
//
// It takes the dispatcher rather than reaching for it through a package variable, so
// two dispatchers in one test run cannot reach each other's held proposal.
type handler func(ctx context.Context, d *dispatcher, args string) (string, error)

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
}

// newDispatcherFor builds a dispatcher over a configuration.
func newDispatcherFor(cfg config.Config) *dispatcher {
	d := &dispatcher{cfg: cfg, newClient: cloudflare.New}
	d.commands = map[string]handler{
		"cloudflare": func(ctx context.Context, d *dispatcher, args string) (string, error) {
			return d.cloudflare(ctx, args)
		},
	}
	return d
}

// Run executes a typed line.
//
// A command the dispatcher does not have is reported by name, and the two cases are
// told apart. The table in internal/tui lists thirty names and this build implements
// one, so a reader who typed /copy is told the name is in the table and this build does
// not run it, rather than being told there is no such command: those are different
// faults and a reader told the second goes looking for a typo.
func (d *dispatcher) Run(ctx context.Context, line string) (string, error) {
	name, args, ok := tui.IsCommand(line)
	if !ok {
		return "", nil
	}

	h, known := d.commands[name]
	if !known {
		if _, listed := tui.Lookup(name); listed {
			return "", fmt.Errorf("/%s is in the table but this build does not run it yet", name)
		}
		return "", fmt.Errorf("unknown command /%s", name)
	}

	return h(ctx, d, args)
}

// cloudflareKey returns the credential the Cloudflare commands make calls with.
//
// It is read from the block rather than held as a field, so the configuration is the one
// place the key lives and there is no second copy to fall out of step with the file.
func (d *dispatcher) cloudflareKey() (string, error) {
	return d.cfg.CloudflareAPIKey()
}

// cloudflareClient returns the API client, building it on first use.
//
// The client is built rather than handed in, since a handler that took one would have
// every caller construct it, and a caller that forgot the credential filter would be a
// caller whose diagnostics carry the key.
func (d *dispatcher) cloudflareClient() (*cloudflare.APIClient, error) {
	if d.client != nil {
		return d.client, nil
	}

	key, err := d.cloudflareKey()
	if err != nil {
		return nil, err
	}
	d.client = d.newClient(key)
	return d.client, nil
}

// cloudflare is the handler for /cloudflare.
//
// Every outcome is a result: an unknown sub-command, a missing flag, and an API refusal
// are all text the caller shows, which is what AGENTS.md holds: a call always produces a
// result.
func (d *dispatcher) cloudflare(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) == "" {
		return d.cloudflareUsage(), nil
	}

	action, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	switch action {
	case "confirm":
		return d.cloudflareConfirm(ctx)
	case "dns":
		return d.cloudflareDNS(ctx, rest)
	case "zone", "cache", "page-rules", "account":
		return "", fmt.Errorf("/cloudflare %s is in the design and not built yet", action)
	default:
		return "", fmt.Errorf("/cloudflare %s is not a sub-command", action)
	}
}

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
func (d *dispatcher) cloudflareDNS(ctx context.Context, args string) (string, error) {
	client, err := d.cloudflareClient()
	if err != nil {
		return "", err
	}

	verb, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	parsed, err := cloudflare.ParseDNS(verb, strings.TrimSpace(rest), dnsFlags)
	if err != nil {
		return "", err
	}

	zone, err := parsed.Require("zone")
	if err != nil {
		return "", err
	}

	switch parsed.Verb {
	case "list":
		records, err := client.ListRecords(zone)
		if err != nil {
			return "", err
		}
		return renderRecords(zone, records), nil

	case "add", "edit", "delete":
		return d.propose(client, parsed, zone)

	default:
		return "", fmt.Errorf("/cloudflare dns %s is not a sub-command", parsed.Verb)
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
func (d *dispatcher) cloudflareConfirm(ctx context.Context) (string, error) {
	if d.pending == nil {
		return "", fmt.Errorf("there is nothing to confirm: /cloudflare proposes a change first")
	}

	client, err := d.cloudflareClient()
	if err != nil {
		return "", err
	}

	p := *d.pending
	d.pending = nil

	switch p.Action {
	case cloudflare.AddRecord:
		record, err := client.CreateRecord(p.Zone, p.Proposed)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("added %s %s in %s as %s",
			record.Type, record.Name, p.Zone, shortID(record.ID)), nil

	case cloudflare.EditRecord:
		record, err := client.UpdateRecord(p.Zone, p.RecordID, p.Proposed)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("changed %s in %s to %s", record.Name, p.Zone, record.Content), nil

	case cloudflare.DeleteRecord:
		if err := client.DeleteRecord(p.Zone, p.RecordID); err != nil {
			return "", err
		}
		return fmt.Sprintf("removed %s from %s", p.Name, p.Zone), nil

	default:
		return "", fmt.Errorf("the held change is not one this build knows")
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
