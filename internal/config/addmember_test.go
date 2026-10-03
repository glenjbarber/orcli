package config

import "testing"

// TestAddMemberKeepsEveryMember is a direct check on the append path, since a
// writer that adds a member and loses the others is the worst failure this
// package can have: the file still parses, and the credential is gone.
//
// The nested cases are the ones that matter. A file whose last member is an
// object or an array is the shape a reader's configuration takes the moment
// something writes a setting that is itself a container, and finding the closing
// brace by searching for the last one puts the new member inside that container
// rather than beside it.
func TestAddMemberKeepsEveryMember(t *testing.T) {
	cases := []struct {
		name string
		in   string
		key  string
		val  string
		want string
	}{
		{
			name: "a file indented over several lines",
			in:   "{\n  \"api_key\": \"k\"\n}",
			key:  "color",
			val:  "true",
			want: "{\n  \"api_key\": \"k\",\n  \"color\": true\n}",
		},
		{
			name: "a file whose last member is an object",
			in:   "{\n  \"api_key\": \"k\",\n  \"theme\": {\n    \"fg\": \"#fff\"\n  }\n}",
			key:  "color",
			val:  "true",
			want: "{\n  \"api_key\": \"k\",\n  \"theme\": {\n    \"fg\": \"#fff\"\n  }," +
				"\n  \"color\": true\n}",
		},
		{
			name: "a file whose last member is an array",
			in:   "{\n  \"api_key\": \"k\",\n  \"trusted\": [\n    \"/a\"\n  ]\n}",
			key:  "color",
			val:  "true",
			want: "{\n  \"api_key\": \"k\",\n  \"trusted\": [\n    \"/a\"\n  ]," +
				"\n  \"color\": true\n}",
		},
		{
			name: "a file carrying a brace inside a string",
			in:   "{\n  \"api_key\": \"k\",\n  \"note\": \"a } brace\"\n}",
			key:  "color",
			val:  "true",
			want: "{\n  \"api_key\": \"k\",\n  \"note\": \"a } brace\"," +
				"\n  \"color\": true\n}",
		},
		{
			name: "a one line file keeps its line and its spacing",
			in:   `{"api_key":"k","model":"m"}`,
			key:  "color",
			val:  "true",
			want: `{"api_key":"k","model":"m","color":true}`,
		},
		{
			name: "a one line file whose members carry a space after the colon",
			in:   `{"api_key": "k"}`,
			key:  "color",
			val:  "true",
			want: `{"api_key": "k","color": true}`,
		},
		{
			name: "a one line file whose last member is a nested object",
			in:   `{"theme":{"fg":"#fff"}}`,
			key:  "color",
			val:  "true",
			want: `{"theme":{"fg":"#fff"},"color":true}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(addMember([]byte(c.in), c.key, c.val))
			if got != c.want {
				t.Errorf("addMember gave\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// TestAddMemberOnAnEmptyObject covers the one case where there is no line to keep
// and no comma to place before the new member, so the object is laid out over
// several lines. That is the form InstallDefault writes.
func TestAddMemberOnAnEmptyObject(t *testing.T) {
	got := string(addMember([]byte(`{}`), "color", "true"))
	if want := "{\n  \"color\": true\n}"; got != want {
		t.Errorf("addMember gave\n%s\nwant\n%s", got, want)
	}
}

// TestAddMemberKeepsTheFileIndentation covers the rule that a reader who wrote a
// file in tabs does not get it back in spaces, and the reverse.
//
// The indent unit is read off the file rather than fixed, since the two are not
// interchangeable to somebody who chose one of them deliberately.
func TestAddMemberKeepsTheFileIndentation(t *testing.T) {
	const withTabs = "{\n\t\"api_key\": \"k\"\n}"
	const wantTabs = "{\n\t\"api_key\": \"k\",\n\t\"color\": true\n}"

	if got := string(addMember([]byte(withTabs), "color", "true")); got != wantTabs {
		t.Errorf("addMember gave\n%q\nwant\n%q", got, wantTabs)
	}
}

// TestAddMemberKeepsTheTail checks what comes after the closing brace. A file a
// reader wrote with no final newline must not acquire one from a command that was
// asked to set something, and a file with two must not lose one.
func TestAddMemberKeepsTheTail(t *testing.T) {
	const body = "{\n  \"api_key\": \"k\"\n}"

	for _, tail := range []string{"", "\n", "\n\n"} {
		got := string(addMember([]byte(body+tail), "color", "true"))
		want := "{\n  \"api_key\": \"k\",\n  \"color\": true\n}" + tail
		if got != want {
			t.Errorf("tail %q gave\n%s\nwant\n%s", tail, got, want)
		}
	}
}

// TestAddMemberKeepsTheTailOnAOneLineFile covers the same rule on the branch with
// no line ends of its own, since a tail is the one thing that branch could have
// dropped while copying the line across.
func TestAddMemberKeepsTheTailOnAOneLineFile(t *testing.T) {
	for _, tail := range []string{"", "\n", "\n\n"} {
		got := string(addMember([]byte(`{"api_key":"k"}`+tail), "color", "true"))
		want := `{"api_key":"k","color":true}` + tail
		if got != want {
			t.Errorf("tail %q gave\n%s\nwant\n%s", tail, got, want)
		}
	}
}

// TestAddMemberLeavesTheMembersItDoesNotKnow checks that a member carrying bytes
// this package has never seen is carried across rather than dropped, which is the
// whole reason the append works on bytes and not on a decoded value.
func TestAddMemberLeavesTheMembersItDoesNotKnow(t *testing.T) {
	const in = "{\n  \"api_key\": \"k\",\n  \"something\": [1, 2, {\"deep\": true}]\n}"

	got := string(addMember([]byte(in), "color", "true"))
	if !has(got, `"something": [1, 2, {"deep": true}]`) {
		t.Errorf("the member was not carried across:\n%s", got)
	}
}

// has reports whether s carries sub. It is a helper for the tests above rather
// than a call into strings, so that a failure names the whole rendered row.
func has(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestClosingBraceFindsTheOutermostOne is the check on the scanner itself, since
// the failure it prevents is silent: a wrong index here produces a file that
// parses and has lost every member before the nested one.
func TestClosingBraceFindsTheOutermostOne(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`{}`, 1},
		{`{"a":1}`, 6},
		{`{"a":{"b":1}}`, 12},
		{`{"a":[1,2]}`, 10},
		{`{"a":{"b":{"c":1}}}`, 18},
		{`{"a":"}"}`, 8},
		{`{"a":"\\"}"}`, 9},
		{`not an object`, -1},
	}

	for _, c := range cases {
		if got := closingBrace([]byte(c.in)); got != c.want {
			t.Errorf("closingBrace(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
