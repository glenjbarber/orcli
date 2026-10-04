package cloudflare

import (
	"strings"
	"testing"
)

// existing is the record a proposal is made against.
func existing() Record {
	return Record{
		ID:      "rec1",
		Type:    "A",
		Name:    "app.example.com",
		Content: "198.51.100.7",
		TTL:     300,
	}
}

// TestADiffShowsTheWholeRecordForACreation covers the case with no prior state,
// which has to render as an addition rather than as nothing.
func TestADiffShowsTheWholeRecordForACreation(t *testing.T) {
	p := Proposal{
		Action:   AddRecord,
		Zone:     "example.com",
		Name:     "new.example.com",
		Proposed: Record{Type: "A", Name: "new.example.com", Content: "203.0.113.42", TTL: 300},
	}

	diff := p.Diff(Record{}, false)

	if !strings.Contains(diff, "+  content: 203.0.113.42") {
		t.Errorf("the new record is not in the diff:\n%s", diff)
	}
	if !strings.Contains(diff, "+  name: new.example.com") {
		t.Errorf("the name is not in the diff:\n%s", diff)
	}
	if strings.Contains(diff, "-  ") {
		t.Errorf("a creation shows removals:\n%s", diff)
	}
}

// TestADiffShowsTheChangeForAnEdit is the case the design's example shows, and the
// one a reader is most likely to be looking at.
func TestADiffShowsTheChangeForAnEdit(t *testing.T) {
	p := Proposal{
		Action:   EditRecord,
		Zone:     "example.com",
		RecordID: "rec1",
		Proposed: existing(),
	}
	p.Proposed.Content = "203.0.113.42"

	diff := p.Diff(existing(), true)

	if !strings.Contains(diff, "-  content: 198.51.100.7") {
		t.Errorf("the old content is not shown as removed:\n%s", diff)
	}
	if !strings.Contains(diff, "+  content: 203.0.113.42") {
		t.Errorf("the new content is not shown as added:\n%s", diff)
	}
	if strings.Contains(diff, "-  ttl:") {
		t.Errorf("an unchanged field is shown as a change:\n%s", diff)
	}
	if !strings.Contains(diff, "   ttl: 300") {
		t.Errorf("an unchanged field is not shown at all:\n%s", diff)
	}
}

// TestADiffShowsTheRecordForADeletion covers the case where only the prior state
// exists to show.
func TestADiffShowsTheRecordForADeletion(t *testing.T) {
	p := Proposal{
		Action:   DeleteRecord,
		Zone:     "example.com",
		RecordID: "rec1",
		Name:     "app.example.com",
	}

	diff := p.Diff(existing(), true)

	if !strings.Contains(diff, "-  content: 198.51.100.7") {
		t.Errorf("the record is not shown as removed:\n%s", diff)
	}
	if strings.Contains(diff, "+  ") {
		t.Errorf("a deletion shows additions:\n%s", diff)
	}
}

// TestADiffNamesTheZoneAndTheRecord covers the header, since a reader looking at
// two diffs needs to tell which zone each is for.
func TestADiffNamesTheZoneAndTheRecord(t *testing.T) {
	p := Proposal{
		Action:   DeleteRecord,
		Zone:     "example.com",
		RecordID: "rec1",
		Name:     "app.example.com",
	}

	diff := p.Diff(existing(), true)

	if !strings.HasPrefix(diff, "--- zone: example.com   record: app.example.com\n") {
		t.Errorf("the diff does not open by naming the zone and the record:\n%s", diff)
	}
}

// TestAProposalThatChangesNothingIsNotOffered covers the edit against itself, which
// a reader should be told about rather than shown a diff of a record against the
// same record.
func TestAProposalThatChangesNothingIsNotOffered(t *testing.T) {
	p := Proposal{Action: EditRecord, Zone: "example.com", RecordID: "rec1", Proposed: existing()}

	if p.Change(existing(), true) {
		t.Error("an edit against the record as it stands was offered as a change")
	}
}

// TestAProposalThatChangesSomethingIsOffered is the ordinary case.
func TestAProposalThatChangesSomethingIsOffered(t *testing.T) {
	p := Proposal{Action: EditRecord, Zone: "example.com", RecordID: "rec1", Proposed: existing()}
	p.Proposed.Content = "203.0.113.42"

	if !p.Change(existing(), true) {
		t.Error("a change was not offered to the reader")
	}
}

// TestTheIdentifierIsNotAChange covers the trap: the identifier is what the
// endpoint assigns, so comparing it would report a change on every write.
func TestTheIdentifierIsNotAChange(t *testing.T) {
	before := existing()
	after := existing()
	after.ID = "rec-different"

	p := Proposal{Action: EditRecord, Zone: "example.com", RecordID: "rec1", Proposed: after}

	if p.Change(before, true) {
		t.Error("a change of identifier alone was reported as a change")
	}
}

// TestACreationWhereTheRecordIsThereIsNotOffered covers the case where the zone and
// the name name different things and a reader would otherwise create a duplicate.
func TestACreationWhereTheRecordIsThereIsNotOffered(t *testing.T) {
	p := Proposal{
		Action:   AddRecord,
		Zone:     "example.com",
		Name:     "app.example.com",
		Proposed: existing(),
	}

	if p.Change(existing(), true) {
		t.Error("a creation where a record of that name is there was offered")
	}
}

// TestADeletionOfNothingIsNotOffered covers the mirror case.
func TestADeletionOfNothingIsNotOffered(t *testing.T) {
	p := Proposal{Action: DeleteRecord, Zone: "example.com", RecordID: "rec1", Name: "app.example.com"}

	if p.Change(Record{}, false) {
		t.Error("a deletion of a record that is not there was offered")
	}
}

// TestTheSameChangeReadsTheSameBothWays covers the field order being fixed: a diff
// whose line order moved between two fetches of an unchanged record would show a
// reader a change that was not made.
func TestTheSameChangeReadsTheSameBothWays(t *testing.T) {
	p := Proposal{Action: DeleteRecord, Zone: "example.com", RecordID: "rec1", Name: "app.example.com"}
	other := Proposal{Action: DeleteRecord, Zone: "example.com", RecordID: "rec1", Name: "app.example.com"}

	if p.Diff(existing(), true) != other.Diff(existing(), true) {
		t.Error("two renderings of the same change differ")
	}
}

// TestActionReadsAsAVerb covers the word a reader sees in the diff header.
func TestActionReadsAsAVerb(t *testing.T) {
	for action, want := range map[Action]string{
		AddRecord:    "add",
		EditRecord:   "edit",
		DeleteRecord: "delete",
	} {
		if got := action.String(); got != want {
			t.Errorf("action %d reads as %q, want %q", action, got, want)
		}
	}
}
