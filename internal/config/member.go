package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// InstallDefault writes the default configuration, and only when it is absent.
//
// It is one of the three writers, and the only one that creates the file. A file
// made by a command would hold no credential, and would suppress the first-time
// setup that tells a reader what to do.
//
// An existing file is left exactly as it is, whatever state it is in. No merge is
// attempted: a partial merge of a credential file can produce a file that parses
// but is wrong, and doing nothing is the reversible outcome.
func InstallDefault() error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("config: read %s: %w", path, err)
	}

	data, err := marshalIndent(Default())
	if err != nil {
		return fmt.Errorf("config: encode the default: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return fmt.Errorf("config: create %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	// The mode is set again rather than relied on from the open. O_EXCL with a
	// mode is subject to the process umask, and the result should not depend on
	// what the reader happens to have set.
	if err := f.Chmod(FileMode); err != nil {
		return fmt.Errorf("config: set the mode on %s: %w", path, err)
	}
	return nil
}

// EnsureAPIKeyStub adds an explicit empty api_key member when the configuration
// file has no api_key member. It leaves an existing member, including an empty
// one, and every unrelated byte untouched.
func EnsureAPIKeyStub(path string) error {
	if err := checkMode(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if !isObject(data) {
		return fmt.Errorf("config: %s: %w", path, ErrNotAnObject)
	}
	if _, found := findMember(data, "api_key"); found {
		return nil
	}
	const emptyString = `""`
	edited, err := setMember(data, "api_key", emptyString)
	if err != nil {
		return err
	}
	if err := checkMember(edited, "api_key", emptyString); err != nil {
		return fmt.Errorf("config: the edit did not verify: %w", err)
	}
	return osWriteFile(path, edited)
}

// AddTrusted records directories in the trusted list.
//
// It is one of the runtime writers, alongside AddTrusted and WriteColor. It
// is a writer here rather than in the command that asked, because the file holds a
// credential and a writer outside this package is a second thing that can corrupt
// it. The edit copies the file as bytes rather than re-encoding it, for the reason
// WriteColor gives: key order, whitespace, escapes and a missing final newline all
// survive.
//
// A directory already listed is not added twice. Trust is asked once per directory,
// and a list carrying the same directory twice is a file a reader has to edit by
// hand before they believe it.
//
// A file that refuses the other writers refuses this one. A trust record is not a
// reason to write to a file another account can read.
func AddTrusted(path string, dirs []string) error {
	if err := checkMode(path); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if !isObject(data) {
		return fmt.Errorf("config: %s: %w", path, ErrNotAnObject)
	}

	existing, err := readTrusted(data)
	if err != nil {
		return fmt.Errorf("config: %s: %s: %w", path, trustedKey, err)
	}

	added := 0
	for _, dir := range dirs {
		if dir == "" || containsDir(existing, dir) {
			continue
		}
		existing = append(existing, dir)
		added++
	}
	if added == 0 {
		// Nothing to record. The file is left exactly as it was, since a writer
		// that rewrites a file in order to change nothing loses the reader's hand
		// for no reason at all.
		return nil
	}

	rendered := renderStrings(existing)
	edited, err := setMember(data, trustedKey, rendered)
	if err != nil {
		return err
	}
	if err := checkMember(edited, trustedKey, rendered); err != nil {
		return fmt.Errorf("config: the edit did not verify: %w", err)
	}
	return osWriteFile(path, edited)
}

// trustedKey is the member the trust record lives under.
//
// It is named once so a writer and a reader cannot disagree about it. The spelling
// is this program's rather than the endpoint's, since the record is about what this
// client was allowed to do in a directory and nothing to do with who answers a
// request.
const trustedKey = "ORCLI_TRUSTED"

// readTrusted reads the trust list out of configuration bytes.
//
// The whole object is decoded rather than the member sliced out by span. A span is
// the right tool for replacing a member, whose value is known and whose boundaries
// the scanner has already settled; it is the wrong tool for reading a value whose
// extent depends on nesting, where a slice taken at the wrong end is a silent
// corruption rather than an error.
func readTrusted(data []byte) ([]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	member, found := raw[trustedKey]
	if !found {
		return nil, nil
	}

	var list []string
	if err := json.Unmarshal(member, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// containsDir reports whether dir is already in the list, compared as written.
//
// The comparison is on the string rather than on a cleaned absolute path, unlike the
// check in the gate. Here the question is whether this exact entry has been
// added, and rewriting a reader's entry to a cleaned form would change a file they
// are reading rather than adding to it.
func containsDir(list []string, dir string) bool {
	for _, entry := range list {
		if entry == dir {
			return true
		}
	}
	return false
}

// renderStrings renders a list the way it is written to disk.
//
// Each entry is quoted as a JSON string rather than pasted in, since a directory is
// a path and a path can carry a backslash, a quote or a character outside ASCII on
// any of the systems this builds for.
func renderStrings(list []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, entry := range list {
		if i > 0 {
			b.WriteString(", ")
		}
		quoted, err := json.Marshal(entry)
		if err != nil {
			// A string cannot fail to marshal. Quoting by hand here would be the one
			// place in the file writing an unescaped path, so the impossible case
			// writes something valid rather than a path with a stray quote in it.
			quoted = []byte(`""`)
		}
		b.Write(quoted)
	}
	b.WriteByte(']')
	return b.String()
}

// DefaultPath is the path InstallDefault writes to.
//
// It is the first entry of the search order, resolved. InstallDefault writes there
// rather than searching, because a search that found a second file and then created
// a third would leave a reader with two and no way to tell which one wins.
func DefaultPath() (string, error) {
	if len(SearchOrder) == 0 {
		return "", errors.New("config: the search order is empty")
	}
	return resolve(SearchOrder[0])
}

// Path returns the path of the configuration file, for a message that names it.
//
// It is the first entry of the search order whether or not a file exists there, so
// a message naming where the file would go is more useful than one saying nothing.
func Path() string {
	path, err := DefaultPath()
	if err != nil {
		return ""
	}
	return path
}

// WriteColor sets the top-level color key.
//
// It is the third of the three writers. It does not re-encode the file: it copies
// the file exactly as bytes and changes the value of one member, or adds one in the
// file's own style, so key order, whitespace, escapes and a missing final newline
// all survive. A configuration file is something a reader edits by hand, and a
// rewritten file loses the hand.
//
// The edit is checked to parse and to hold the new value before anything is
// written. A colour change that corrupted the credential file would be worse than a
// colour change that did not happen.
func WriteColor(path string, on bool) error {
	if err := checkMode(path); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if !isObject(data) {
		return fmt.Errorf("config: %s: %w", path, ErrNotAnObject)
	}

	value := "false"
	if on {
		value = "true"
	}
	edited, err := setMember(data, "color", value)
	if err != nil {
		return err
	}
	if err := checkMember(edited, "color", value); err != nil {
		return fmt.Errorf("config: the edit did not verify: %w", err)
	}
	return osWriteFile(path, edited)
}

// setMember changes one top-level member, or adds one, preserving the rest.
//
// The file is edited as bytes rather than through a decode and an encode, since an
// encode is what loses everything the file carried that this client does not name.
func setMember(data []byte, key, value string) ([]byte, error) {
	if s, found := findMember(data, key); found {
		out := make([]byte, 0, len(data))
		out = append(out, data[:s.start]...)
		out = append(out, value...)
		out = append(out, data[s.end:]...)
		return out, nil
	}
	return addMember(data, key, value), nil
}

// addMember puts a member last in an object, at the shape the file already uses.
//
// Everything after the closing brace is carried across unchanged. A file with no
// final newline must not acquire one from a command that was asked to set a colour,
// and a file with two newlines at the end must not lose one: the reader wrote the
// file, and the shape of what they wrote is not this package's to tidy.
//
// The closing brace is found by a scan rather than by a search, for the reason
// closingBrace gives. The indentation is read off the file rather than assumed,
// for the reason indent gives. Neither is decoration: both decide where the new
// member lands, and a member that lands inside a neighbouring object is a file that
// parses and has lost the credential.
func addMember(data []byte, key, value string) []byte {
	last := closingBrace(data)
	if last < 0 {
		// Not an object. The writers check for this before calling, and a caller
		// that skipped the check gets the file back unchanged rather than a
		// corrupted one.
		return data
	}

	// tail is whatever followed the closing brace, including whether it was empty.
	// It is appended rather than assumed.
	tail := data[last+1:]

	// head is the object without its closing brace, with the whitespace in front of
	// that brace removed.
	head := bytes.TrimRight(data[:last], "\r\n \t")

	// An object written entirely on one line has no indentation to copy and no line
	// to put a member on, so the new member goes beside the others on that same
	// line. Laying the object out one member to a line would be tidier and is not
	// what this package does: a command that reformats a file the reader wrote by
	// hand has edited that file rather than added to it, and the reason every writer
	// here works on bytes rather than on a decoded value is that the reader's
	// spacing is part of what they wrote.
	if !bytes.Contains(head, []byte("\n")) {
		return addMemberToOneLine(head, tail, key, value)
	}

	// The levels are counted from the outermost brace rather than read off the last
	// line of the file, since the last line is wherever the file happens to end and
	// that has nothing to do with how deep the new member belongs.
	levels := depthAt(data[:last])

	out := make([]byte, 0, len(data)+len(key)+len(value)+16)
	out = append(out, head...)
	out = append(out, ',', '\n')
	out = append(out, indent(data, levels)...)
	out = append(out, '"')
	out = append(out, key...)
	out = append(out, '"', ':', ' ')
	out = append(out, value...)
	out = append(out, '\n')
	out = append(out, indent(data, levels-1)...)
	out = append(out, '}')
	out = append(out, tail...)
	return out
}

// addMemberToOneLine appends a member to an object written on a single line.
//
// The members already there are left exactly where they are and on the line they
// were on, and the new one goes after a comma beside them. An object holding
// nothing is the one case with no line to preserve, so it is laid out over several,
// which is the form InstallDefault writes.
func addMemberToOneLine(head, tail []byte, key, value string) []byte {
	// The opening brace is trimmed off so what is left is the members and the commas
	// between them. An object holding nothing is the case where there is no comma to
	// place before the new member.
	inner := strings.TrimSpace(string(head[1:]))
	if inner == "" {
		return []byte(fmt.Sprintf("{\n  %q: %s\n}%s", key, value, tail))
	}

	// The spacing after the colon is copied from a member already in the object, so
	// a file written without one does not gain one and a file written with one keeps
	// it. It is the same reasoning as copying the indentation: the reader's spacing
	// is theirs.
	spaced := strings.Contains(inner, `": `)

	out := make([]byte, 0, len(head)+len(key)+len(value)+8)
	out = append(out, '{')
	out = append(out, head[1:]...)
	out = append(out, ',', '"')
	out = append(out, key...)
	out = append(out, '"', ':')
	if spaced {
		out = append(out, ' ')
	}
	out = append(out, value...)
	out = append(out, '}')
	out = append(out, tail...)
	return out
}

// closingBrace returns where the outermost object ends, or -1.
//
// It is found by a scan that tracks nesting and string state rather than by
// searching for the last brace. A search finds the brace of the last nested value,
// so adding a member to a file whose last member is an object would write the new
// member inside that object and leave the outer one truncated: a file that parses,
// is missing every member before the nested one, and holds no credential.
//
// The scan begins outside the object, so the opening brace takes the depth to one
// and the index returned is the one whose matching brace brings the depth back to
// zero. Returning on any closer instead would answer with the first brace entered,
// which is the opening one, and no outer brace would ever be found, so every append
// would take the single-line branch.
func closingBrace(data []byte) int {
	depth := 0
	inStr := false
	escaped := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}

		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// depthAt reports how deeply nested the scan is at the end of a prefix.
//
// It is what the indentation is worked out from: a member written beside the
// outermost brace sits one level in from it, and the brace that closes it sits at
// the outer level itself.
//
// It is a second scan rather than a second value from closingBrace, because the
// depth at the outer brace is always one. A caller asking for a figure with one
// answer is a sign the question belongs to the function that already has it.
func depthAt(data []byte) int {
	depth := 0
	inStr := false
	escaped := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}

		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

// indent returns n levels of this file's indentation.
//
// A file indented with tabs keeps tabs and one indented with spaces keeps spaces,
// and the two are not interchangeable to a reader who chose one of them
// deliberately. The unit is read off the first line carrying leading whitespace,
// since a line with none says nothing about it and a blank line says it about
// nothing.
func indent(data []byte, levels int) []byte {
	if levels < 0 {
		levels = 0
	}
	unit := "  "
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || len(trimmed) == len(line) {
			continue
		}
		unit = line[:len(line)-len(trimmed)]
		break
	}
	return []byte(strings.Repeat(unit, levels))
}

// span is the byte range of a top-level member within an object: from just after
// its name and colon to the end of its value.
type span struct {
	start int
	end   int
}

// findMember returns the span of the value of a top-level member.
//
// The object is walked once, tracking nesting and string state, so a brace or a
// quote inside a nested value or a string cannot be mistaken for the end of the
// object. A member whose value is itself an object or an array ends at its
// matching bracket rather than at the first comma inside it, which is what makes it
// safe to set a member in a file whose neighbours are containers.
//
// The span starts after the colon and its whitespace, so a replacement keeps the
// spacing the file already had rather than taking the spacing of this package.
func findMember(data []byte, key string) (span, bool) {
	var (
		members   = map[string]span{}
		name      string
		nameFrom  int
		valueFrom int
		haveName  bool

		depth   int
		inStr   bool
		escaped bool
	)

	// record ends the member whose name has been read, if the scan is at depth one.
	// A member of a nested object is not a member of this one.
	record := func(at int) {
		if depth == 1 && haveName {
			members[name] = span{start: valueFrom, end: at}
			haveName = false
		}
	}

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
				if depth == 1 && !haveName {
					// nameFrom points just past the opening quote, so the name is the
					// bytes between the two quotes.
					name = string(data[nameFrom:i])
				}
			}
			continue
		}

		switch c {
		case '"':
			inStr = true
			nameFrom = i + 1

		case ':':
			if depth == 1 {
				valueFrom = i + 1
				for valueFrom < len(data) &&
					(data[valueFrom] == ' ' || data[valueFrom] == '\t') {
					valueFrom++
				}
				haveName = true
			}

		case '{', '[':
			depth++

		case '}', ']':
			record(i)
			if depth > 0 {
				depth--
			}

		case ',':
			record(i)
		}
	}

	if haveName {
		members[name] = span{start: valueFrom, end: len(data)}
	}
	if s, ok := members[key]; ok {
		return s, true
	}
	return span{}, false
}

// checkMember reports whether the edited bytes parse as an object holding the new
// value.
func checkMember(edited []byte, key, value string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(edited, &raw); err != nil {
		return err
	}
	got, found := raw[key]
	if !found {
		return fmt.Errorf("the member %q is not in the edited file", key)
	}
	if string(bytes.TrimSpace(got)) != value {
		return fmt.Errorf("the member %q is %s, want %s", key, got, value)
	}
	return nil
}
