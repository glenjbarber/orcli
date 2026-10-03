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
// An existing file is left exactly as it is, whatever state it is in. No merge
// is attempted: a partial merge of a credential file can produce a file that
// parses but is wrong, and doing nothing is the reversible outcome.
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

// DefaultPath is the path InstallDefault writes to.
//
// It is the first entry of the search order, resolved. InstallDefault writes
// there rather than searching, because a search that found a second file and
// then created a third would leave a reader with two and no way to tell which
// one wins.
func DefaultPath() (string, error) {
	if len(SearchOrder) == 0 {
		return "", errors.New("config: the search order is empty")
	}
	return resolve(SearchOrder[0])
}

// Path returns the path of the configuration file, for a message that names it.
//
// It is the first entry of the search order whether or not a file exists there,
// so a message naming where the file would go is more useful than one saying
// nothing.
func Path() string {
	path, err := DefaultPath()
	if err != nil {
		return ""
	}
	return path
}

// WriteColor sets the top-level color key.
//
// It is one of the three writers. It does not re-encode the file: it copies the
// file exactly as bytes and changes the value of one member, or adds one in the
// file's own style, so key order, whitespace, escapes and a missing final
// newline all survive. A configuration file is something a reader edits by hand,
// and a rewritten file loses the hand.
//
// The edit is checked to parse and to hold the new value before anything is
// written. A colour change that corrupted the credential file would be worse
// than a colour change that did not happen.
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
// The file is edited as bytes rather than through a decode and an encode, since
// an encode is what loses everything the file carried that this client does not
// name.
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

// addMember puts a member last in an object, at the indentation the file uses.
//
// Everything after the closing brace is carried across unchanged. A file with no
// final newline must not acquire one from a command that was asked to set a
// colour, and a file with two newlines at the end must not lose one: the reader
// wrote the file, and the shape of what they wrote is not this package's to
// tidy.
func addMember(data []byte, key, value string) []byte {
	last := bytes.LastIndexByte(data, '}')
	if last < 0 {
		// Not an object. WriteColor checks for this before calling, and a
		// caller that skipped the check gets the file back unchanged rather
		// than a corrupted one.
		return data
	}

	// tail is whatever followed the closing brace, including whether it was
	// empty. It is appended rather than assumed.
	tail := data[last+1:]

	// An object on a single line has no indentation to copy, so the two-space
	// form is used. It is the form InstallDefault writes.
	if !bytes.Contains(data[:last], []byte("\n")) {
		return []byte(fmt.Sprintf("{\n  %q: %s\n}%s", key, value, tail))
	}

	indent := []byte(closingIndent(data))

	out := make([]byte, 0, len(data)+len(indent)+len(key)+len(value)+8)
	out = append(out, bytes.TrimRight(data[:last], "\r\n \t")...)
	out = append(out, ',', '\n')
	out = append(out, indent...)
	out = append(out, '"')
	out = append(out, key...)
	out = append(out, '"', ':', ' ')
	out = append(out, value...)
	out = append(out, '\n')
	out = append(out, indent...)
	out = append(out, '}')
	out = append(out, tail...)
	return out
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
// matching bracket rather than at the first comma inside it, which is what makes
// it safe to set a member in a file whose neighbours are containers.
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

	// record ends the member whose name has been read, if the scan is at depth
	// one. A member of a nested object is not a member of this one.
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
					// nameFrom points just past the opening quote, so the
					// name is the bytes between the two quotes.
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

// closingIndent returns the whitespace before the closing brace of an object.
//
// The indentation is read off the last line that is not the closing brace, so a
// file indented with tabs keeps tabs and one indented with spaces keeps spaces.
func closingIndent(data []byte) string {
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := string(lines[i])
		if strings.HasPrefix(strings.TrimSpace(line), "}") {
			continue
		}
		return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	}
	return ""
}

// checkMember reports whether the edited bytes parse as an object holding the
// new value.
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
