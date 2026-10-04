package cloudflare

import (
	"fmt"
	"strings"
)

// Action is what a proposal would do to a record.
type Action int

const (
	// AddRecord creates a record that is not there.
	AddRecord Action = iota
	// EditRecord changes a record that is there.
	EditRecord
	// DeleteRecord removes a record that is there.
	DeleteRecord
)

// String is the verb a reader reads.
func (a Action) String() string {
	switch a {
	case AddRecord:
		return "add"
	case EditRecord:
		return "edit"
	case DeleteRecord:
		return "delete"
	default:
		return "change"
	}
}

// Proposal is one change a reader is being asked to confirm.
//
// It holds the arguments rather than a closure, since a change held between two
// commands has to survive the reader typing something else in between, and a
// closure holding a client is a thing the next command would have to reach
// through.
type Proposal struct {
	// Action is what the change would do.
	Action Action

	// Zone is the zone the record is in.
	Zone string

	// RecordID is the record being changed or removed. It is empty for a
	// creation.
	RecordID string

	// Name is the record name, carried on a creation and on a deletion where
	// the record is named rather than identified.
	Name string

	// Proposed is the record as it would stand after the change. It is what
	// the diff is made from and what is sent.
	Proposed Record
}

// Change reports whether the proposal would alter anything.
//
// It is asked before the proposal is offered, since a reader who typed an edit
// that changes nothing has nothing to confirm, and a diff of a record against
// itself is noise where an answer is not.
//
// The two cases it rules out are a creation where a record of that name is
// already there and a change or a deletion where it is not. Both are what a
// reader gets when the zone and the name name different things, and both would
// otherwise produce a diff against nothing that reads as a real change.
func (p Proposal) Change(before Record, existed bool) bool {
	if existed == (p.Action == AddRecord) {
		return false
	}
	if p.Action == DeleteRecord {
		return true
	}
	return !sameRecord(before, p.Proposed)
}

// sameRecord reports whether two records would read the same to a reader.
//
// The identifier is not compared. It is what the endpoint assigns, so comparing
// it would report a change on every write and tell a reader they are changing
// something they are not.
func sameRecord(a, b Record) bool {
	return a.Type == b.Type &&
		a.Name == b.Name &&
		a.Content == b.Content &&
		a.TTL == b.TTL &&
		a.Proxied == b.Proxied
}

// Diff renders the change as a unified diff against the current state.
//
// It is plain text on purpose. The confirmation is the one place in the interface
// where the reader is deciding something, and colour there would be arguing with
// them; the - and + marks are what a reader already reads in a diff.
//
// A creation renders as a whole-record addition and a deletion as the removal of
// the record as it stands, since neither has both sides to show.
func (p Proposal) Diff(before Record, existed bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "--- zone: %s   record: %s\n", p.Zone, labelFor(p, existed))
	fmt.Fprintf(&b, "+++ zone: %s   record: %s\n", p.Zone, labelFor(p, existed))
	fmt.Fprintf(&b, "--- a/cloudflare/dns/%s\n", p.Action)
	fmt.Fprintf(&b, "+++ b/cloudflare/dns/%s\n", p.Action)

	switch {
	case !existed:
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(p.Proposed.Fields()))
		for _, f := range p.Proposed.Fields() {
			fmt.Fprintf(&b, "+  %s: %s\n", f.Name, f.Value)
		}
	case p.Action == DeleteRecord:
		fmt.Fprintf(&b, "@@ -1,%d +0,0 @@\n", len(before.Fields()))
		for _, f := range before.Fields() {
			fmt.Fprintf(&b, "-  %s: %s\n", f.Name, f.Value)
		}
	default:
		fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(before.Fields()), len(p.Proposed.Fields()))
		beforeSet := fieldValues(before.Fields())
		for _, f := range p.Proposed.Fields() {
			if old, ok := beforeSet[f.Name]; ok && old == f.Value {
				fmt.Fprintf(&b, "   %s: %s\n", f.Name, f.Value)
				continue
			}
			if old, ok := beforeSet[f.Name]; ok {
				fmt.Fprintf(&b, "-  %s: %s\n", f.Name, old)
			}
			fmt.Fprintf(&b, "+  %s: %s\n", f.Name, f.Value)
		}
	}
	return b.String()
}

// labelFor names the record in the diff header.
func labelFor(p Proposal, existed bool) string {
	if p.Name != "" {
		return p.Name
	}
	if existed {
		return p.Proposed.Name
	}
	return p.RecordID
}

// fieldValues indexes a record's fields by name.
func fieldValues(fields []Field) map[string]string {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		out[f.Name] = f.Value
	}
	return out
}
