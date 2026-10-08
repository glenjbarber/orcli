package groq

import (
	"context"
	"testing"
)

// TestKeyUsageIsUnsupported covers the one behavior [Client.KeyUsage] has:
// it always reports that Groq publishes no usage accounting, rather than
// guessing at a shape or claiming a key has spent nothing.
func TestKeyUsageIsUnsupported(t *testing.T) {
	c := New("gsk-test")
	info, err := c.KeyUsage(context.Background())
	if err != ErrKeyUsageUnsupported {
		t.Errorf("KeyUsage returned %v, want ErrKeyUsageUnsupported", err)
	}
	if info.Label != "" || info.Usage != 0 {
		t.Errorf("KeyUsage returned %+v alongside the error, want the zero value", info)
	}
}
