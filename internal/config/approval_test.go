package config

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestApprovalModeIsReadFromTheFile covers the three modes you named.
func TestApprovalModeIsReadFromTheFile(t *testing.T) {
	h := home(t)

	for _, mode := range []string{"ask", "allow", "deny"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(h, ".orcli.json")
			writeConfig(t, path,
				`{"api_key":"sk-or-v1-abc","approval":"`+mode+`"}`, 0o600)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got, err := cfg.ApprovalMode()
			if err != nil {
				t.Fatalf("ApprovalMode: %v", err)
			}
			if got.String() != mode {
				t.Errorf("mode is %q, want %q", got, mode)
			}
		})
	}
}

// TestApprovalDefaultsToAsk covers a file written before the mode existed.
//
// Asking is what it did then, and a file that does not mention the mode is a file
// that predates the mode rather than one that declined to set it.
func TestApprovalDefaultsToAsk(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")
	writeConfig(t, path, `{"api_key":"sk-or-v1-abc"}`, 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mode, err := cfg.ApprovalMode()
	if err != nil {
		t.Fatalf("ApprovalMode: %v", err)
	}
	if mode != ApprovalAsk {
		t.Errorf("mode is %q, want %q", mode, ApprovalAsk)
	}
}

// TestAnUnknownApprovalModeIsAFault covers the file written by hand.
//
// A reader who named a mode this client does not know meant something by it, and
// substituting a default would silently do something other than what they wrote.
func TestAnUnknownApprovalModeIsAFault(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")
	writeConfig(t, path, `{"api_key":"sk-or-v1-abc","approval":"refuse"}`, 0o600)

	if _, err := Load(); !errors.Is(err, ErrBadApproval) {
		t.Errorf("Load returned %v, want ErrBadApproval for the mode %q", err, "refuse")
	}
}

// TestApprovalAcceptsAnyCase covers a reader who did not think about case.
func TestApprovalAcceptsAnyCase(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")
	writeConfig(t, path, `{"api_key":"sk-or-v1-abc","approval":"  ALLOW  "}`, 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mode, err := cfg.ApprovalMode()
	if err != nil {
		t.Fatalf("ApprovalMode: %v", err)
	}
	if mode != ApprovalAllow {
		t.Errorf("mode is %q, want %q", mode, ApprovalAllow)
	}
}

// TestApprovalModesAreTheThreeYouNamed pins the set.
func TestApprovalModesAreTheThreeYouNamed(t *testing.T) {
	if len(ApprovalModes) != 3 {
		t.Fatalf("there are %d modes, want 3", len(ApprovalModes))
	}
	for _, want := range []Approval{ApprovalAsk, ApprovalAllow, ApprovalDeny} {
		if !want.Records() {
			t.Errorf("%q is not a mode this client knows", want)
		}
	}
	if Approval("refuse").Records() {
		t.Error("refuse is accepted as a mode, want it rejected")
	}
}

// TestReadableIsReadFromTheFile covers the read-only reach.
func TestReadableIsReadFromTheFile(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")
	writeConfig(t, path,
		`{"api_key":"sk-or-v1-abc","ORCLI_READABLE":["/usr/share/doc"]}`, 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Readable) != 1 || cfg.Readable[0] != "/usr/share/doc" {
		t.Errorf("Readable = %v, want [/usr/share/doc]", cfg.Readable)
	}
}

// TestDefaultCarriesNoReadableRoots covers what a first run grants.
//
// Nothing is readable outside the working directory until a reader says so, since a
// default that granted anything would be a grant nobody made.
func TestDefaultCarriesNoReadableRoots(t *testing.T) {
	if got := Default().Readable; len(got) != 0 {
		t.Errorf("the default grants %v, want nothing", got)
	}
}
