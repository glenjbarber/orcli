package clipboard

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestSequenceIsAnOSC52 covers the shape a terminal acts on.
//
// The payload is standard base64 and the sequence names the clipboard selection c,
// which is the one every terminal answers to.
func TestSequenceIsAnOSC52(t *testing.T) {
	got, err := Sequence("hello")
	if err != nil {
		t.Fatalf("Sequence: %v", err)
	}

	if !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Errorf("the sequence is %q, want it to start with an OSC 52 for the c selection", got)
	}
	if !strings.HasSuffix(got, "\a\x1b\\") {
		t.Errorf("the sequence is %q, want it terminated by both forms", got)
	}
}

// TestThePayloadRoundTrips is the property that matters.
//
// Whatever else is true of the sequence, the bytes a terminal decodes have to be
// the text that was put in.
func TestThePayloadRoundTrips(t *testing.T) {
	for _, text := range []string{
		"",
		"hello",
		"a line\nand another",
		"tabs\tand spaces   ",
		"unicode: café, 日本語, an em dash — and a quote \"",
		"a very long line that a terminal might fold or scroll through",
	} {
		t.Run(text[:min(len(text), 12)], func(t *testing.T) {
			got, err := Sequence(text)
			if err != nil {
				t.Fatalf("Sequence: %v", err)
			}

			// Take what is between the selector and the terminator.
			encoded := got[len("\x1b]52;c;") : len(got)-len("\a\x1b\\")]

			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("the payload is not base64: %v", err)
			}
			if string(decoded) != text {
				t.Errorf("the payload decoded to %q, want %q", decoded, text)
			}
		})
	}
}

// TestEmptyTextIsWritten covers the case a reader reaches by accident.
//
// An empty copy is refused by the caller rather than here, so this is only checking
// that the encoding does not fail on a zero length.
func TestEmptyTextIsWritten(t *testing.T) {
	got, err := Sequence("")
	if err != nil {
		t.Fatalf("Sequence with no text: %v", err)
	}

	encoded := got[len("\x1b]52;c;") : len(got)-len("\a\x1b\\")]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the payload is not base64: %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("the payload decoded to %q, want nothing", decoded)
	}
}

// TestAPayloadPastTheLimitIsRefusedByName covers the limit.
//
// A payload this client knows is too large is refused rather than handed to a
// terminal that will drop it without saying so, and the message names the figure
// so a reader is not left guessing.
func TestAPayloadPastTheLimitIsRefusedByName(t *testing.T) {
	tooBig := strings.Repeat("x", maxPayload+1)

	got, err := Sequence(tooBig)
	if err == nil {
		t.Fatalf("a payload of %d bytes was written, want it refused", len(tooBig))
	}
	if got != "" {
		t.Errorf("a refused payload produced %q, want nothing written", got)
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("the failure is %q, want it to name the limit", err)
	}
}

// TestAPayloadAtTheLimitIsWritten covers the boundary.
//
// The limit is where writing stops being refused, not one below it.
func TestAPayloadAtTheLimitIsWritten(t *testing.T) {
	atLimit := strings.Repeat("x", maxPayload)

	got, err := Sequence(atLimit)
	if err != nil {
		t.Fatalf("a payload of exactly the limit was refused: %v", err)
	}
	if got == "" {
		t.Error("a payload at the limit produced no sequence")
	}
}

// TestThePayloadIsStandardBase64 covers the encoding choice.
//
// URL-safe base64 would be shorter on some terminals and wrong on all of them.
func TestThePayloadIsStandardBase64(t *testing.T) {
	// Bytes that encode differently under the two alphabets: 0xfb 0xef is ++++
	// under standard and ---- under URL-safe.
	text := "\xfb\xef\xfe"

	got, err := Sequence(text)
	if err != nil {
		t.Fatalf("Sequence: %v", err)
	}

	encoded := got[len("\x1b]52;c;") : len(got)-len("\a\x1b\\")]
	if !strings.ContainsAny(encoded, "+/") {
		t.Errorf("the payload is %q, want the standard alphabet rather than the URL-safe one",
			encoded)
	}
	if _, err := base64.URLEncoding.DecodeString(encoded); err == nil {
		t.Error("the payload decodes as URL-safe base64, want standard only")
	}
}

// TestMaxPayloadIsReadable covers the accessor, so a message can name the limit
// without reaching for the constant.
func TestMaxPayloadIsReadable(t *testing.T) {
	if MaxPayload() != maxPayload {
		t.Errorf("MaxPayload is %d, want %d", MaxPayload(), maxPayload)
	}
}
