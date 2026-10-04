package cloudflare

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fake returns a server that answers every request with the given body, and the
// client pointed at it.
func fake(t *testing.T, status int, body string) *APIClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := New("secret-key")
	c.Base = srv.URL
	return c
}

// recordsBody is a zone holding one record, which is the shape every list call
// decodes.
const recordsBody = `{"success":true,"result":[
	{"id":"rec1","type":"A","name":"app.example.com","content":"198.51.100.7","ttl":300,"proxied":false}
]}`

// TestListRecordsDecodesWhatTheEndpointSent is the case the command leans on: a
// record the endpoint named has to come back with its fields, or a reader is shown
// a list with nothing in it.
func TestListRecordsDecodesWhatTheEndpointSent(t *testing.T) {
	c := fake(t, http.StatusOK, recordsBody)

	records, err := c.ListRecords("example.com")
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	want := Record{ID: "rec1", Type: "A", Name: "app.example.com", Content: "198.51.100.7", TTL: 300}
	if records[0] != want {
		t.Errorf("the record came back as %+v, want %+v", records[0], want)
	}
}

// TestListRecordsReportsAnEmptyZoneAsEmpty is the first-run case. A zone with no
// records is a thing a reader has, and a nil slice would read as a call that
// returned nothing.
func TestListRecordsReportsAnEmptyZoneAsEmpty(t *testing.T) {
	c := fake(t, http.StatusOK, `{"success":true,"result":[]}`)

	records, err := c.ListRecords("example.com")
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if records == nil {
		t.Error("an empty zone came back as nil, want an empty slice")
	}
	if len(records) != 0 {
		t.Errorf("got %d records, want 0", len(records))
	}
}

// TestListRecordsNeedsAZone covers the refusal that names what is missing.
func TestListRecordsNeedsAZone(t *testing.T) {
	c := fake(t, http.StatusOK, recordsBody)

	if _, err := c.ListRecords(""); err != ErrNoZone {
		t.Errorf("a call with no zone returned %v, want ErrNoZone", err)
	}
}

// TestFindRecordRefusesAnAmbiguousName is the case where picking one silently
// would show a diff against a record the reader did not name.
func TestFindRecordRefusesAnAmbiguousName(t *testing.T) {
	c := fake(t, http.StatusOK, `{"success":true,"result":[
		{"id":"rec1","type":"A","name":"*.example.com","content":"198.51.100.7","ttl":300},
		{"id":"rec2","type":"CNAME","name":"*.example.com","content":"app.example.com","ttl":300}
	]}`)

	_, _, err := c.FindRecord("example.com", "*.example.com")
	if err == nil {
		t.Fatal("two records at one name were reported as one")
	}
	if !strings.Contains(err.Error(), "--record-id") {
		t.Errorf("the refusal is %q, want it to say how to name one", err)
	}
}

// TestStatusErrorCarriesWhatTheEndpointSaid is the whole reason the body is kept:
// Cloudflare names the fault, and a reader told only "400" is a reader who has to
// go and find out why.
func TestStatusErrorCarriesWhatTheEndpointSaid(t *testing.T) {
	c := fake(t, http.StatusBadRequest, `{"success":false,"errors":[{"code":1004,"message":"invalid DNS name"}]}`)

	_, err := c.ListRecords("example.com")
	if err == nil {
		t.Fatal("a 400 was reported as a success")
	}

	var se *statusError
	if !asStatusError(err, &se) {
		t.Fatalf("the failure is %T, want a statusError", err)
	}
	if se.Code != 400 {
		t.Errorf("the code is %d, want 400", se.Code)
	}
	if se.Reason != "invalid DNS name" {
		t.Errorf("the reason is %q, want what the endpoint said", se.Reason)
	}
}

// TestStatusErrorReadsAMessagesList covers the second shape Cloudflare reports
// under, since a reader who got the wrong one is told nothing at all.
func TestStatusErrorReadsAMessagesList(t *testing.T) {
	c := fake(t, http.StatusBadRequest, `{"success":false,"messages":[{"code":9103,"message":"Unknown X-Ray error"}]}`)

	_, err := c.ListRecords("example.com")

	var se *statusError
	if !asStatusError(err, &se) {
		t.Fatalf("the failure is %T, want a statusError", err)
	}
	if se.Reason != "Unknown X-Ray error" {
		t.Errorf("the reason is %q, want what the endpoint said", se.Reason)
	}
}

// TestTheCredentialDoesNotSurviveIntoAnError is the rule a log row depends on: a
// row is a thing a reader selects and pastes somewhere else, and an endpoint that
// quoted the request back has put the key in it.
func TestTheCredentialDoesNotSurviveIntoAnError(t *testing.T) {
	c := fake(t, http.StatusBadRequest,
		`{"success":false,"errors":[{"code":6003,"message":"Invalid credentials: secret-key"}]}`)

	_, err := c.ListRecords("example.com")
	if err == nil {
		t.Fatal("a 400 was reported as a success")
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Errorf("the credential survived into an error: %q", err)
	}
}

// TestFilterRemovesTheCredential is the rule itself, at the boundary where a string
// becomes something a reader sees.
func TestFilterRemovesTheCredential(t *testing.T) {
	c := New("sk-live-abc123")

	got := c.Filter("the request carried sk-live-abc123 and failed")
	if strings.Contains(got, "sk-live-abc123") {
		t.Errorf("the credential survived the filter: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("the filter left no mark: %q", got)
	}
}

// TestFilterLeavesAStringAlone covers the common case, which must cost the least
// and must not rewrite a string that never carried the credential.
func TestFilterLeavesAStringAlone(t *testing.T) {
	c := New("sk-live-abc123")

	const s = "a zone with no records"
	if got := c.Filter(s); got != s {
		t.Errorf("a string that carried no credential came back as %q", got)
	}
}

// TestFilterWithNoCredentialIsIdentity covers the client built for a reader who
// has not put a key in the file yet.
func TestFilterWithNoCredentialIsIdentity(t *testing.T) {
	c := New("")

	const s = "anything"
	if got := c.Filter(s); got != s {
		t.Errorf("a client with no credential changed a string: %q", got)
	}
}

// TestANilClientFiltersNothing covers the zero value, since a client built by hand
// is a call site a reader does not see and must not panic on.
func TestANilClientFiltersNothing(t *testing.T) {
	var c *APIClient

	const s = "anything"
	if got := c.Filter(s); got != s {
		t.Errorf("a nil client changed a string: %q", got)
	}
}

// dnsKnown is the flag table the tests parse against. It is the same set the
// dispatcher uses, so a flag the tests exercise is one a reader can type.
var dnsKnown = map[string]bool{
	"zone": true, "name": true, "type": true,
	"content": true, "ttl": true, "record-id": true, "proxied": true,
}

// TestParseDNSReadsFlagsBothWays covers the two spellings a reader types, since a
// parser that accepted one would refuse the other for no reason a reader can see.
func TestParseDNSReadsFlagsBothWays(t *testing.T) {
	a, err := ParseDNS("add", "--name app.example.com --content=203.0.113.42", dnsKnown)
	if err != nil {
		t.Fatalf("ParseDNS: %v", err)
	}
	if a.Verb != "add" {
		t.Errorf("the action is %q, want add", a.Verb)
	}
	if got, _ := a.Get("name"); got != "app.example.com" {
		t.Errorf("--name is %q", got)
	}
	if got, _ := a.Get("content"); got != "203.0.113.42" {
		t.Errorf("--content is %q", got)
	}
}

// TestParseDNSRefusesAnUnknownFlag is the case where ignoring it would leave a
// reader believing they had set something they had not.
func TestParseDNSRefusesAnUnknownFlag(t *testing.T) {
	_, err := ParseDNS("add", "--name app.example.com --weight 5", dnsKnown)
	if err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if !strings.Contains(err.Error(), "--weight") {
		t.Errorf("the refusal is %q, want it to name the flag", err)
	}
}

// TestParseDNSRefusesAFlagWithNoValue covers an empty value being sent to the
// endpoint as though the reader had asked for it.
func TestParseDNSRefusesAFlagWithNoValue(t *testing.T) {
	if _, err := ParseDNS("add", "--name", dnsKnown); err == nil {
		t.Fatal("a flag with no value was accepted")
	}
}

// TestParseDNSRefusesAFlagFollowedByAnotherFlag covers a value being taken from
// the next field whatever it looks like, so a name beginning with two dashes is a
// name a reader typed rather than a missing value.
func TestParseDNSRefusesAFlagFollowedByAnotherFlag(t *testing.T) {
	if _, err := ParseDNS("add", "--name app --ttl", dnsKnown); err == nil {
		t.Fatal("a trailing flag with no value was accepted")
	}
}

// TestParseDNSNeedsAVerb covers an empty verb.
func TestParseDNSNeedsAVerb(t *testing.T) {
	if _, err := ParseDNS("  ", "", dnsKnown); err != ErrUnknownSub {
		t.Errorf("an empty verb returned %v, want ErrUnknownSub", err)
	}
}

// TestRequireNamesTheMissingFlag covers the message a reader gets when they typed
// the sub-command and forgot the value.
func TestRequireNamesTheMissingFlag(t *testing.T) {
	a, err := ParseDNS("add", "--zone example.com", dnsKnown)
	if err != nil {
		t.Fatalf("ParseDNS: %v", err)
	}

	if _, err := a.Require("content"); err == nil {
		t.Fatal("a missing flag was accepted")
	} else if !strings.Contains(err.Error(), "--content") {
		t.Errorf("the refusal is %q, want it to name the flag", err)
	}
}

// TestRequireRefusesAnEmptyValue covers the reader who typed the flag and gave it
// nothing.
func TestRequireRefusesAnEmptyValue(t *testing.T) {
	a := Args{Verb: "add", set: map[string]string{"content": ""}}

	if _, err := a.Require("content"); err == nil {
		t.Fatal("an empty value was accepted")
	}
}

// TestTTLRefusesSomethingThatIsNotANumber covers a reader who typed "three hundred"
// and would otherwise be told by the endpoint about the value rather than the flag.
func TestTTLRefusesSomethingThatIsNotANumber(t *testing.T) {
	a := Args{Verb: "add", set: map[string]string{"ttl": "three hundred"}}

	if _, err := a.TTL(); err == nil {
		t.Fatal("a TTL that is not a number was accepted")
	}
}

// TestTTLRefusesZero covers a reader who typed a TTL that would expire every lookup
// the moment the record resolved.
func TestTTLRefusesZero(t *testing.T) {
	a := Args{Verb: "add", set: map[string]string{"ttl": "0"}}

	if _, err := a.TTL(); err == nil {
		t.Fatal("a TTL of zero was accepted")
	}
}

// TestTTLDefaultsToUnset covers the ordinary case: a reader who did not name one
// gets zero here and the endpoint applies its own default.
func TestTTLDefaultsToUnset(t *testing.T) {
	a := Args{Verb: "add", set: map[string]string{}}

	n, err := a.TTL()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if n != 0 {
		t.Errorf("an unset TTL came back as %d, want 0", n)
	}
}

// TestApplyLeavesAFlagTheReaderDidNotName covers the property the whole handler
// rests on: a reader who typed --content is saying what to change and is not saying
// the TTL becomes zero.
func TestApplyLeavesAFlagTheReaderDidNotName(t *testing.T) {
	a, err := ParseDNS("edit", "--content 203.0.113.42", dnsKnown)
	if err != nil {
		t.Fatalf("ParseDNS: %v", err)
	}

	r := Record{Type: "A", Name: "app.example.com", Content: "198.51.100.7", TTL: 300, Proxied: true}
	if err := a.Apply(&r); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if r.Content != "203.0.113.42" {
		t.Errorf("the content came back as %q", r.Content)
	}
	if r.TTL != 300 {
		t.Errorf("the TTL became %d, and the reader did not name it", r.TTL)
	}
	if !r.Proxied {
		t.Error("proxied was cleared, and the reader did not name it")
	}
}

// TestApplyRefusesATypeThisClientDoesNotKnow covers the check that keeps a mistyped
// type from reaching the endpoint, where the reader would be told about the value
// rather than the flag.
func TestApplyRefusesATypeThisClientDoesNotKnow(t *testing.T) {
	a, err := ParseDNS("add", "--type CNMAE", dnsKnown)
	if err != nil {
		t.Fatalf("ParseDNS: %v", err)
	}

	r := Record{}
	if err := a.Apply(&r); err == nil {
		t.Fatal("a type this client does not know was accepted")
	}
}

// TestApplyRefusesAProxiedThatIsNotOnOrOff covers the same case for a boolean the
// reader mistyped.
func TestApplyRefusesAProxiedThatIsNotOnOrOff(t *testing.T) {
	a, err := ParseDNS("add", "--proxied perhaps", dnsKnown)
	if err != nil {
		t.Fatalf("ParseDNS: %v", err)
	}

	if err := a.Apply(&Record{}); err == nil {
		t.Fatal("a proxied that is neither on nor off was accepted")
	}
}

// TestActionMapsTheThreeWrites covers the mapping the handler relies on, including
// that a read is no action at all rather than a creation.
func TestActionMapsTheThreeWrites(t *testing.T) {
	for verb, want := range map[string]Action{
		"add": AddRecord, "edit": EditRecord, "delete": DeleteRecord,
	} {
		a, err := ParseDNS(verb, "", dnsKnown)
		if err != nil {
			t.Fatalf("ParseDNS %s: %v", verb, err)
		}
		if got := a.Action(); got != want {
			t.Errorf("%s is %v, want %v", verb, got, want)
		}
	}
}

// TestTypeKnown pins the list against a type that is not one, since a check that
// accepted everything would pass every test above.
func TestTypeKnown(t *testing.T) {
	for _, name := range []string{"A", "AAAA", "CNAME", "TXT", "MX"} {
		if !TypeKnown(name) {
			t.Errorf("%s is not in the list of accepted types", name)
		}
	}
	for _, name := range []string{"CNMAE", "a", "SOA"} {
		if TypeKnown(name) {
			t.Errorf("%s is accepted, and it is not a type this client names", name)
		}
	}
}

// asStatusError is errors.As without importing errors into every test.
func asStatusError(err error, target **statusError) bool {
	for err != nil {
		if se, ok := err.(*statusError); ok {
			*target = se
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
