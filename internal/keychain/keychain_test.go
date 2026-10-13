package keychain

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// withFakeRunner replaces run for the duration of one test and restores it
// afterward, so tests never touch a real Keychain (and pass identically on
// Linux CI, which has no `security` binary at all).
func withFakeRunner(t *testing.T, fn func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)) {
	t.Helper()
	old := run
	run = fn
	t.Cleanup(func() { run = old })
}

// withDarwin forces goos to "darwin" for the duration of one test, so the
// platform-gated paths are reachable from any host running this suite.
func withDarwin(t *testing.T) {
	t.Helper()
	old := goos
	goos = "darwin"
	t.Cleanup(func() { goos = old })
}

// fakeExitError builds an *exec.ExitError carrying the given exit code,
// the only shape isNotFound actually inspects.
func fakeExitError(t *testing.T, code int) error {
	t.Helper()
	// The simplest portable way to obtain a real *exec.ExitError with a
	// chosen exit code is to actually run a shell that exits with it -
	// this still never touches the Keychain or any fake of it.
	cmd := exec.Command("sh", "-c", "exit "+itoa(code))
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an *exec.ExitError from a deliberately failing command, got %v (%T)", err, err)
	}
	return exitErr
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestLookup_UnsupportedOffDarwin(t *testing.T) {
	old := goos
	goos = "linux"
	t.Cleanup(func() { goos = old })

	if _, err := Lookup(context.Background(), Ref{Account: "a", Service: "s"}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Lookup() on a non-darwin goos = %v, want ErrUnsupported", err)
	}
}

func TestLookup_IncompleteRefIsRefusedWithoutRunningAnything(t *testing.T) {
	withDarwin(t)
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		t.Fatal("run was called for an incomplete reference; it should have been refused first")
		return nil, nil, nil
	})

	for _, ref := range []Ref{{}, {Account: "only-account"}, {Service: "only-service"}} {
		if _, err := Lookup(context.Background(), ref); !errors.Is(err, ErrIncompleteRef) {
			t.Errorf("Lookup(%+v) = %v, want ErrIncompleteRef", ref, err)
		}
	}
}

func TestLookup_SuccessReturnsTheSecretTrimmedOfItsTrailingNewline(t *testing.T) {
	withDarwin(t)
	ref := Ref{Account: "john@smith.com", Service: "private-cloudflare-api"}
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		if name != "security" {
			t.Errorf("program = %q, want security", name)
		}
		want := []string{"find-generic-password", "-a", ref.Account, "-s", ref.Service, "-w"}
		if len(args) != len(want) {
			t.Fatalf("args = %v, want %v", args, want)
		}
		for i, a := range want {
			if args[i] != a {
				t.Errorf("args[%d] = %q, want %q", i, args[i], a)
			}
		}
		return []byte("super-secret-token\n"), nil, nil
	})

	got, err := Lookup(context.Background(), ref)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got != "super-secret-token" {
		t.Errorf("Lookup() = %q, want %q", got, "super-secret-token")
	}
}

func TestLookup_NotFoundByExitCode(t *testing.T) {
	withDarwin(t)
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("SecKeychainSearchCopyNext: The specified item could not be found in the keychain."),
			fakeExitError(t, 44)
	})

	_, err := Lookup(context.Background(), Ref{Account: "a", Service: "s"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Lookup() error = %v, want ErrNotFound", err)
	}
}

// TestLookup_FailureNeverLeaksStdout is the load-bearing test in this
// package (issue #67's own acceptance criteria): a command that fails after
// having already written something to stdout - a shape real `security`
// should never produce, but this proves the package does not trust that -
// must not let that stdout reach the returned error.
func TestLookup_FailureNeverLeaksStdout(t *testing.T) {
	withDarwin(t)
	const leakCandidate = "THIS-MUST-NEVER-APPEAR-IN-AN-ERROR"
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte(leakCandidate), []byte("some other diagnostic"), fakeExitError(t, 1)
	})

	_, err := Lookup(context.Background(), Ref{Account: "a", Service: "s"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), leakCandidate) {
		t.Errorf("the returned error leaked stdout: %v", err)
	}
}

func TestLookup_GenericFailureNamesTheReferenceNotTheSecret(t *testing.T) {
	withDarwin(t)
	ref := Ref{Account: "john@smith.com", Service: "private-cloudflare-api"}
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("Keychain could not be unlocked."), fakeExitError(t, 1)
	})

	_, err := Lookup(context.Background(), ref)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "john@smith.com") || !strings.Contains(err.Error(), "private-cloudflare-api") {
		t.Errorf("error does not identify the lookup reference: %v", err)
	}
	if !strings.Contains(err.Error(), "could not be unlocked") {
		t.Errorf("error does not carry the diagnostic: %v", err)
	}
}

func TestSet_SendsTheSecretAsOneArgument(t *testing.T) {
	withDarwin(t)
	ref := Ref{Account: "a", Service: "s"}
	var gotArgs []string
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		gotArgs = args
		return nil, nil, nil
	})

	if err := Set(context.Background(), ref, "the-secret"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	want := []string{"add-generic-password", "-a", "a", "-s", "s", "-w", "the-secret", "-U"}
	if len(gotArgs) != len(want) {
		t.Fatalf("args = %v, want %v", gotArgs, want)
	}
	for i, a := range want {
		if gotArgs[i] != a {
			t.Errorf("args[%d] = %q, want %q", i, gotArgs[i], a)
		}
	}
}

func TestSet_FailureNeverLeaksTheSecret(t *testing.T) {
	withDarwin(t)
	const secret = "THIS-MUST-NEVER-APPEAR-IN-AN-ERROR"
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("some diagnostic"), fakeExitError(t, 1)
	})

	err := Set(context.Background(), Ref{Account: "a", Service: "s"}, secret)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the returned error leaked the secret: %v", err)
	}
}

func TestSet_IncompleteRefIsRefusedWithoutRunningAnything(t *testing.T) {
	withDarwin(t)
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		t.Fatal("run was called for an incomplete reference; it should have been refused first")
		return nil, nil, nil
	})

	if err := Set(context.Background(), Ref{Account: "only-account"}, "x"); !errors.Is(err, ErrIncompleteRef) {
		t.Errorf("Set() = %v, want ErrIncompleteRef", err)
	}
}

func TestDelete_RemovesExactlyTheNamedItem(t *testing.T) {
	withDarwin(t)
	ref := Ref{Account: "a", Service: "s"}
	var gotArgs []string
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		gotArgs = args
		return nil, nil, nil
	})

	if err := Delete(context.Background(), ref); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	want := []string{"delete-generic-password", "-a", "a", "-s", "s"}
	if len(gotArgs) != len(want) {
		t.Fatalf("args = %v, want %v", gotArgs, want)
	}
	for i, a := range want {
		if gotArgs[i] != a {
			t.Errorf("args[%d] = %q, want %q", i, gotArgs[i], a)
		}
	}
}

func TestDelete_NotFound(t *testing.T) {
	withDarwin(t)
	withFakeRunner(t, func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("could not be found in the keychain"), fakeExitError(t, 44)
	})

	if err := Delete(context.Background(), Ref{Account: "a", Service: "s"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() = %v, want ErrNotFound", err)
	}
}

func TestRef_String_NeverPanicsOnUnusualNames(t *testing.T) {
	ref := Ref{Account: `weird" account`, Service: "svc; rm -rf /"}
	s := ref.String()
	if !strings.Contains(s, "account") || !strings.Contains(s, "svc") {
		t.Errorf("String() = %q, want it to contain the unusual names verbatim", s)
	}
}
