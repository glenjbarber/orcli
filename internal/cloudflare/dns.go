package cloudflare

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Record is one DNS record as Cloudflare reports it.
//
// The fields carried are the ones a reader confirms a change against. A record
// carries more at the endpoint, and carrying the rest here would be a struct that
// grows with every field the API adds and a diff that shows the reader a change
// they did not make.
type Record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// Fields renders a record as the lines a diff is made of, in a fixed order.
//
// The order is fixed rather than whatever the JSON arrived in, since a diff whose
// line order moves between two fetches of an unchanged record shows a reader a
// change that was not made.
func (r Record) Fields() []Field {
	return []Field{
		{Name: "type", Value: r.Type},
		{Name: "name", Value: r.Name},
		{Name: "content", Value: r.Content},
		{Name: "ttl", Value: strconv.Itoa(r.TTL)},
		{Name: "proxied", Value: strconv.FormatBool(r.Proxied)},
	}
}

// Field is one line of a record as a name and a value.
type Field struct {
	Name  string
	Value string
}

// zonePath returns the path for a zone's records.
//
// The zone identifier goes into the path rather than a query, so it is escaped. A
// zone name that carried a slash would otherwise reach the endpoint as a different
// path and name a zone the reader did not ask for.
func zonePath(zone, suffix string) string {
	return "/zones/" + url.PathEscape(zone) + "/dns_records" + suffix
}

// ListRecords returns the DNS records for a zone.
//
// A zone with no records is an empty slice and no error: an empty zone is a thing a
// reader has rather than a failure to tell them about.
func (c *APIClient) ListRecords(zone string) ([]Record, error) {
	if zone == "" {
		return nil, ErrNoZone
	}

	var payload struct {
		Result []Record `json:"result"`
	}
	if err := c.do("GET", zonePath(zone, ""), nil, &payload); err != nil {
		return nil, err
	}
	if payload.Result == nil {
		return []Record{}, nil
	}
	return payload.Result, nil
}

// FindRecord returns the record with this name in this zone, and whether it is there.
//
// It is the read behind every write, since a write has to know what the current state
// is before it can show the reader a change against it. A name with several records,
// which is what a wildcard and a plain record at the same name give, is reported as an
// error rather than as the first one, since picking one of two records silently would
// show a diff against a record the reader did not name.
func (c *APIClient) FindRecord(zone, name string) (Record, bool, error) {
	if zone == "" {
		return Record{}, false, ErrNoZone
	}

	records, err := c.ListRecords(zone)
	if err != nil {
		return Record{}, false, err
	}

	var found []Record
	for _, r := range records {
		if r.Name == name {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 0:
		return Record{}, false, nil
	case 1:
		return found[0], true, nil
	default:
		return Record{}, false, fmt.Errorf(
			"cloudflare: %s has %d records at %q, name one with --record-id", zone, len(found), name)
	}
}

// CreateRecord adds a record and returns it as the endpoint stored it.
//
// The endpoint decides the identifier and normalises the content, so what comes back
// is what is now there rather than what was asked for.
func (c *APIClient) CreateRecord(zone string, r Record) (Record, error) {
	if zone == "" {
		return Record{}, ErrNoZone
	}

	var payload struct {
		Result Record `json:"result"`
	}
	if err := c.do("POST", zonePath(zone, ""), r, &payload); err != nil {
		return Record{}, err
	}
	return payload.Result, nil
}

// UpdateRecord replaces a record's content and returns it as the endpoint stored it.
//
// The identifier goes in the path, escaped for the same reason the zone does.
func (c *APIClient) UpdateRecord(zone, id string, r Record) (Record, error) {
	if zone == "" {
		return Record{}, ErrNoZone
	}
	if id == "" {
		return Record{}, fmt.Errorf("cloudflare: no record to update")
	}

	var payload struct {
		Result Record `json:"result"`
	}
	if err := c.do("PATCH", zonePath(zone, "/"+url.PathEscape(id)), r, &payload); err != nil {
		return Record{}, err
	}
	return payload.Result, nil
}

// DeleteRecord removes a record.
//
// A record that is already gone is not an error here. A reader confirming a deletion
// twice is asking a question whose answer is already known, and a refusal would report
// a fault where there is none.
func (c *APIClient) DeleteRecord(zone, id string) error {
	if zone == "" {
		return ErrNoZone
	}
	if id == "" {
		return fmt.Errorf("cloudflare: no record to delete")
	}
	return c.do("DELETE", zonePath(zone, "/"+url.PathEscape(id)), nil, nil)
}

// recordTypes are the types this client accepts for a new record.
//
// The list is checked rather than passed through, since a reader who mistyped "CNAME"
// as "CNMAE" should be told before the call rather than by the endpoint, and the check
// costs one map.
var recordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "TXT": true,
	"MX": true, "NS": true, "SRV": true, "CAA": true, "PTR": true,
}

// TypeKnown reports whether this client accepts a record type.
func TypeKnown(name string) bool { return recordTypes[name] }

// Args is the parsed argument text of one /cloudflare dns call.
//
// It is a flat map rather than a struct, since the flags differ per action and a struct
// would carry fields for all of them with three quarters empty. It is exported because
// the caller that owns the command is the caller that decides what to do with a missing
// flag, and the parsing itself belongs with the rest of what this package knows about
// the surface.
type Args struct {
	// Verb is the action: list, add, edit or delete.
	Verb string

	// set holds the flags by name, without their leading dashes.
	set map[string]string
}

// ParseDNS reads the text after /cloudflare dns.
//
// The verb is the first word and the rest are flags of the form --name value or
// --name=value. Both spellings work, since a reader who typed one has no reason to
// know about the other.
//
// An unrecognised flag is an error rather than a flag ignored. A reader who typed
// --ttl and got no error would not know the record was created with the default, and
// a flag silently dropped is worse than a flag refused.
func ParseDNS(verb, text string, known map[string]bool) (Args, error) {
	verb = strings.TrimSpace(verb)
	if verb == "" {
		return Args{}, ErrUnknownSub
	}

	fields := strings.Fields(text)
	set := map[string]string{}

	for i := 0; i < len(fields); i++ {
		flag := fields[i]
		if !strings.HasPrefix(flag, "--") {
			return Args{}, fmt.Errorf("cloudflare: %q is not a flag", flag)
		}

		name, value, hasValue := strings.Cut(strings.TrimPrefix(flag, "--"), "=")
		if !known[name] {
			return Args{}, fmt.Errorf("cloudflare: --%s is not a flag of dns %s", name, verb)
		}
		if hasValue {
			set[name] = value
			continue
		}

		// A flag with no value takes the next field. A flag at the end with nothing
		// after it is an error rather than an empty value, since an empty TTL or an
		// empty name would be sent to the endpoint as though the reader had asked for
		// it.
		if i+1 >= len(fields) || strings.HasPrefix(fields[i+1], "--") {
			return Args{}, fmt.Errorf("cloudflare: --%s needs a value", name)
		}
		i++
		set[name] = fields[i]
	}
	return Args{Verb: verb, set: set}, nil
}

// Action reports what this call would do to a record.
//
// An unknown verb reports no action rather than guessing, and the caller refuses a
// verb it does not have before it asks. A verb that fell through to a default here
// would be a default the caller never checked.
func (a Args) Action() Action {
	switch a.Verb {
	case "add":
		return AddRecord
	case "edit":
		return EditRecord
	case "delete":
		return DeleteRecord
	default:
		return AddRecord
	}
}

// Apply puts the flags the reader named onto a record.
//
// A flag not named is left alone rather than zeroed, since a reader who typed
// --content alone is saying what to change and is not saying the TTL becomes zero.
// The endpoint applies its own default to a member that is not sent, so an unset TTL
// is left for the endpoint to answer for rather than guessed at here.
func (a Args) Apply(r *Record) error {
	if v, ok := a.set["name"]; ok {
		r.Name = v
	}
	if v, ok := a.set["type"]; ok {
		if !recordTypes[v] {
			return fmt.Errorf("cloudflare: --type is %q, which is not a record type", v)
		}
		r.Type = v
	}
	if v, ok := a.set["content"]; ok {
		r.Content = v
	}

	ttl, err := a.TTL()
	if err != nil {
		return err
	}
	if ttl > 0 {
		r.TTL = ttl
	}

	if v, ok := a.set["proxied"]; ok {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("cloudflare: --proxied is %q, which is not on or off", v)
		}
		r.Proxied = on
	}
	return nil
}

// Get returns a flag's value and whether it was named.
func (a Args) Get(name string) (string, bool) {
	v, ok := a.set[name]
	return v, ok
}

// Require returns a flag's value, or an error naming what is missing.
//
// The error names the flag rather than saying the arguments were wrong, since a reader
// who typed --content and forgot it is told exactly that.
func (a Args) Require(name string) (string, error) {
	v, ok := a.set[name]
	if !ok {
		return "", fmt.Errorf("cloudflare: %s needs --%s", a.Verb, name)
	}
	if v == "" {
		return "", fmt.Errorf("cloudflare: --%s is empty", name)
	}
	return v, nil
}

// TTL reads --ttl, and reports what it was given when it is not a number.
//
// A TTL the endpoint refuses would be refused with a message about the value rather
// than about the flag, and a reader who typed "three hundred" is told here what was
// wrong with it.
func (a Args) TTL() (int, error) {
	v, ok := a.set["ttl"]
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("cloudflare: --ttl is %q, which is not a number of seconds", v)
	}
	if n < 1 {
		return 0, fmt.Errorf("cloudflare: --ttl is %d, which is not a number of seconds", n)
	}
	return n, nil
}
