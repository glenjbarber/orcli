package config

import (
	"strings"
	"testing"
)

// TestWriteColorAddsAMember covers the case the design leads with: a file that
// has never carried the key.
func TestWriteColorAddsAMember(t *testing.T) {
	h := home(t)
	path := writeConfigAt(t, h, `{
  "api_key": "sk-or-v1-abc",
  "model": "some/model"
}`)

	if err := WriteColor(path, true); err != nil {
		t.Fatalf("WriteColor: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Color {
		t.Error("Color = false, want true")
	}
	if got, want := cfg.APIKey, "sk-or-v1-abc"; got != want {
		t.Errorf("APIKey = %q, want %q: the edit must not disturb the key", got, want)
	}
	if got, want := cfg.Model, "some/model"; got != want {
		t.Errorf("Model = %q, want %q: the edit must not disturb the model", got, want)
	}
}

// TestWriteColorChangesAMember covers the other case.
func TestWriteColorChangesAMember(t *testing.T) {
	h := home(t)
	path := writeConfigAt(t, h, `{
  "api_key": "sk-or-v1-abc",
  "color": false
}`)

	if err := WriteColor(path, true); err != nil {
		t.Fatalf("WriteColor: %v", err)
	}

	data := readConfig(t, path)
	if !strings.Contains(string(data), `"color": true`) {
		t.Errorf("the file is %s, want the value changed in place", data)
	}
	if strings.Count(string(data), "color") != 1 {
		t.Errorf("the file is %s, want one color member rather than two", data)
	}
}

// TestWriteColorPreservesTheRestOfTheFile is the property the writer exists
// for.
//
// A configuration file is something a reader edits by hand. A rewrite that
// loses key order, an escape, a nested object, or the final newline takes the
// hand with it.
func TestWriteColorPreservesTheRestOfTheFile(t *testing.T) {
	h := home(t)
	body := "{\n" +
		"  \"api_key\": \"sk-or-v1-abc\",\n" +
		"  \"ORCLI_TRUSTED\": [\"/a/b\", \"/c/d\"],\n" +
		"  \"somethingNew\": {\"nested\": [1, 2, {\"deeper\": true}]},\n" +
		"  \"quoted\": \"a, } and a { inside a string\"\n" +
		"}"
	path := writeConfigAt(t, h, body)

	if err := WriteColor(path, false); err != nil {
		t.Fatalf("WriteColor: %v", err)
	}

	data := readConfig(t, path)
	got := string(data)

	for _, want := range []string{
		"\"api_key\": \"sk-or-v1-abc\"",
		"\"ORCLI_TRUSTED\": [\"/a/b\", \"/c/d\"]",
		"\"somethingNew\": {\"nested\": [1, 2, {\"deeper\": true}]}",
		"\"quoted\": \"a, } and a { inside a string\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the edit lost %s\nfile is now %s", want, got)
		}
	}
	if !strings.Contains(got, "\"color\": false") {
		t.Errorf("the file is %s, want the member added", got)
	}
}

// TestWriteColorMatchesTheFilesOwnIndentation checks a tab-indented file does
// not acquire spaces.
func TestWriteColorMatchesTheFilesOwnIndentation(t *testing.T) {
	h := home(t)
	path := writeConfigAt(t, h, "{\n\t\"api_key\": \"sk-or-v1-abc\"\n}")

	if err := WriteColor(path, true); err != nil {
		t.Fatalf("WriteColor: %v", err)
	}

	data := readConfig(t, path)
	if !strings.Contains(string(data), "\n\t\"color\": true") {
		t.Errorf("the file is %q, want the member indented with a tab", data)
	}
}

// TestWriteColorKeepsAMissingFinalNewlineMissing checks a file that has no final
// newline does not acquire one.
func TestWriteColorKeepsAMissingFinalNewlineMissing(t *testing.T) {
	h := home(t)
	path := writeConfigAt(t, h, `{"api_key": "sk-or-v1-abc"}`)

	if err := WriteColor(path, true); err != nil {
		t.Fatalf("WriteColor: %v", err)
	}

	data := readConfig(t, path)
	if strings.HasSuffix(string(data), "\n") {
		t.Errorf("the file is %q, want no final newline added", data)
	}
}

// TestWriteColorKeepsKeyOrder checks the members that were there stay in the
// order they were written.
func TestWriteColorKeepsKeyOrder(t *testing.T) {
	h := home(t)
	path := writeConfigAt(t, h, `{
  "api_key": "sk-or-v1-abc",
  "model": "m",
  "bell": true
}`)

	if err := WriteColor(path, true); err != nil {
		t.Fatalf("WriteColor: %v", err)
	}

	data := string(readConfig(t, path))
	apiAt := strings.Index(data, "api_key")
	modelAt := strings.Index(data, "model")
	bellAt := strings.Index(data, "bell")
	if !(apiAt < modelAt && modelAt < bellAt) {
		t.Errorf("the file is %s, want the original order kept", data)
	}
}

// TestWriteColorRefusesAPermissiveFile covers the mode check before the write.
//
// The writer must not be a way to make a loose file tighter by accident, nor a
// way to write into one that is already loose.
func TestWriteColorRefusesAPermissiveFile(t *testing.T) {
	h := home(t)
	path := writeConfigAt(t, h, `{"api_key": "sk-or-v1-abc"}`)
	relax(t, path, 0o644)

	if err := WriteColor(path, true); err == nil {
		t.Error("WriteColor returned nil, want it to refuse a file at 0644")
	}
}

// TestWriteColorRefusesANonObject covers the shape check.
func TestWriteColorRefusesANonObject(t *testing.T) {
	h := home(t)
	path := writeConfigAt(t, h, `["not", "an", "object"]`)

	if err := WriteColor(path, true); err == nil {
		t.Error("WriteColor returned nil, want it to refuse a file that is not an object")
	}
}

// TestSetMemberAddsToAnEmptyObject covers the two-member object written by
// InstallDefault, since that is the file a first run has.
func TestSetMemberAddsToAnEmptyObject(t *testing.T) {
	got, err := setMember([]byte("{}"), "color", "true")
	if err != nil {
		t.Fatalf("setMember: %v", err)
	}
	if err := checkMember(got, "color", "true"); err != nil {
		t.Errorf("the edit does not hold the value: %v", err)
	}
}

// TestSetMemberReplacesAValueInPlace covers the scanner against a member whose
// value is a container, which is where a naive scan stops at the wrong comma.
func TestSetMemberReplacesAValueInPlace(t *testing.T) {
	body := []byte(`{"a": {"x": 1, "y": 2}, "color": false, "b": [3, 4]}`)

	got, err := setMember(body, "color", "true")
	if err != nil {
		t.Fatalf("setMember: %v", err)
	}
	if err := checkMember(got, "color", "true"); err != nil {
		t.Errorf("the edit does not hold the value: %v", err)
	}

	s := string(got)
	if !strings.Contains(s, `{"x": 1, "y": 2}`) {
		t.Errorf("the edit damaged the neighbour: %s", s)
	}
	if !strings.Contains(s, `"b": [3, 4]`) {
		t.Errorf("the edit damaged the neighbour: %s", s)
	}
}

// TestSetMemberIgnoresANameThatIsAValue checks a string that looks like a key
// is not mistaken for one.
//
// The trusted list holds paths, and a path can contain a brace or a colon.
func TestSetMemberIgnoresANameThatIsAValue(t *testing.T) {
	body := []byte(`{"ORCLI_TRUSTED": ["/a: {weird}/b"], "color": false}`)

	got, err := setMember(body, "color", "true")
	if err != nil {
		t.Fatalf("setMember: %v", err)
	}
	if err := checkMember(got, "color", "true"); err != nil {
		t.Errorf("the edit does not hold the value: %v", err)
	}
	if !strings.Contains(string(got), `"/a: {weird}/b"`) {
		t.Errorf("the edit damaged the neighbour: %s", got)
	}
}
