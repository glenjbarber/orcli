// Package clipboard writes text to a terminal's clipboard.
//
// It is one function and one constant, and it is a package rather than a function
// in the interface because the encoding has decisions in it that are worth naming
// away from the code that draws the frame.
//
// # Why OSC 52 rather than a command
//
// An external clipboard program is a subprocess. On this client a subprocess means
// the approval path and a question about every copy, and it does not work over a
// link where the clipboard belongs to the machine the reader is sitting at rather
// than the one the session runs on. OSC 52 is an escape sequence the terminal itself
// acts on, so it is one write and no process.
//
// # What cannot be known
//
// Writing the sequence is the whole of what a client can do. There is no
// acknowledgement: a terminal that ignores OSC 52 produces no error, and a terminal
// that honours it produces none either. So the function reports that it wrote the
// sequence and nothing more, and a caller must not describe the write as a copy
// having happened.
//
// # Limits
//
// Terminals vary widely in how much they will take. A payload past about 100 KB is
// refused rather than written and silently dropped, since a reader who was told it
// worked and then found an unchanged clipboard has been told something untrue.
//
// A browser-based terminal requires a user gesture for a clipboard write, so the
// sequence may be ignored there even when it is well formed. That is a property of
// the terminal and not something this package can detect.
package clipboard

import (
	"encoding/base64"
	"fmt"
)

// maxPayload is the largest payload written.
//
// The figure is a ceiling rather than a promise: several terminals refuse well
// below it and a few accept more. It is here so that a payload this client knows
// is too large is refused by name rather than handed to a terminal that will drop it
// without saying so.
const maxPayload = 100 * 1024

// Sequence renders text as an OSC 52 sequence.
//
// The payload is standard base64, which is what the specification calls for, and
// the sequence is terminated by a string terminator (BEL) as well as the standard
// form (ST), because a terminal that understands one but not the other is common
// enough that emitting both costs nothing.
func Sequence(text string) (string, error) {
	if len(text) > maxPayload {
		return "", fmt.Errorf("clipboard: the text is %d bytes, and the limit is %d",
			len(text), maxPayload)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + encoded + "\a\x1b\\", nil
}

// MaxPayload is the largest payload written, for a message that names the limit.
func MaxPayload() int { return maxPayload }
