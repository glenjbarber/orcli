package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// cognitoMarker returns the path of the marker under home.
func cognitoMarker(home string) string {
	return filepath.Join(home, ".orcli", cognitoName)
}

// TestCognitoAbsentIsNotHeld covers a home with no marker in it.
//
// A missing marker is an absence and not a failure, so a session with no mode
// held opens and records as it always did. Reporting an error here would make
// every ordinary session a fault.
func TestCognitoAbsentIsNotHeld(t *testing.T) {
	c := Cognito{Home: t.TempDir()}

	held, err := c.Adopt()
	if err != nil {
		t.Fatalf("Adopt = %v, want nil: a marker that is not there is not a fault", err)
	}
	if held {
		t.Error("Adopt = true, want false: no marker was written")
	}
}

// TestCognitoHoldThenAdopt covers the ordinary turn on.
func TestCognitoHoldThenAdopt(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	path, err := c.Hold()
	if err != nil {
		t.Fatalf("Hold = %v, want nil", err)
	}
	if want := cognitoMarker(home); path != want {
		t.Errorf("Hold = %q, want %q", path, want)
	}

	held, err := c.Adopt()
	if err != nil {
		t.Fatalf("Adopt = %v, want nil", err)
	}
	if !held {
		t.Error("Adopt = false, want true: this process wrote the marker")
	}
}

// TestCognitoHoldWritesTheMode covers the mode the marker must carry.
//
// The marker holds no credential, so the mode is not a leak. The reason for it
// is that a marker any account could write is a marker any account could forge,
// and a forged marker is a mode a reader never asked for held over a
// conversation that does get recorded.
func TestCognitoHoldWritesTheMode(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	if _, err := c.Hold(); err != nil {
		t.Fatalf("Hold = %v, want nil", err)
	}

	info, err := os.Stat(cognitoMarker(home))
	if err != nil {
		t.Fatalf("stat the marker: %v", err)
	}
	if perm := info.Mode().Perm(); perm != CognitoFileMode {
		t.Errorf("the marker is mode %#o, want %#o", perm, CognitoFileMode)
	}
}

// TestCognitoHoldWritesTheDirectory covers the mode of the directory holding
// the marker.
//
// The directory holds the sessions and the marker, and neither is any business
// of another account.
func TestCognitoHoldWritesTheDirectory(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	if _, err := c.Hold(); err != nil {
		t.Fatalf("Hold = %v, want nil", err)
	}

	info, err := os.Stat(filepath.Join(home, ".orcli"))
	if err != nil {
		t.Fatalf("stat the directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("the directory is mode %#o, want %#o", perm, 0o700)
	}
}

// TestCognitoHoldReplacesAStaleMarker covers turning the mode on over one a
// crash left behind.
//
// This is the ordinary case rather than an edge, since a session that crashed
// leaves exactly this behind and the reader turning the mode on again is the
// repair. Refusing it would leave the mode unturnable without hand surgery.
func TestCognitoHoldReplacesAStaleMarker(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	// A marker naming a process that is not this one, which is what a crash
	// leaves. The pid is a real one, because a marker naming no process is a
	// different fault and is covered below.
	if err := os.MkdirAll(filepath.Join(home, ".orcli"), 0o700); err != nil {
		t.Fatalf("create the directory: %v", err)
	}
	stale := []byte(strconv.Itoa(os.Getpid() + 1))
	if err := os.WriteFile(cognitoMarker(home), stale, CognitoFileMode); err != nil {
		t.Fatalf("write the stale marker: %v", err)
	}

	if _, err := c.Hold(); err != nil {
		t.Fatalf("Hold = %v, want nil: a stale marker is replaced, not refused", err)
	}

	held, err := c.Adopt()
	if err != nil {
		t.Fatalf("Adopt = %v, want nil", err)
	}
	if !held {
		t.Error("Adopt = false, want true: the marker was replaced")
	}
}

// TestCognitoAdoptReportsAnotherProcess covers a marker this process did not
// write.
//
// A session finding a marker that is not its own has to be able to tell whether
// to honour it or report it. Honouring it is how a reader ends up with a mode
// they never asked for held over a conversation that does get recorded, and
// removing it is not this package's decision to make on the reader's behalf.
func TestCognitoAdoptReportsAnotherProcess(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	if err := os.MkdirAll(filepath.Join(home, ".orcli"), 0o700); err != nil {
		t.Fatalf("create the directory: %v", err)
	}
	other := []byte(strconv.Itoa(os.Getpid() + 1))
	if err := os.WriteFile(cognitoMarker(home), other, CognitoFileMode); err != nil {
		t.Fatalf("write the marker: %v", err)
	}

	held, err := c.Adopt()
	if !errors.Is(err, ErrCognitoStale) {
		t.Fatalf("Adopt = %v, want %v", err, ErrCognitoStale)
	}
	if held {
		t.Error("Adopt = true, want false: a marker of another process is not honoured")
	}
}

// TestCognitoAdoptRefusesAMarkerItCannotRead covers a marker holding something
// other than a process.
//
// A body that is anything longer than a number with whitespace around it is a
// file someone else wrote, and reading it as one of these is how a session
// starts honouring a mode that was never turned on. This is a fault rather than
// an absence for the same reason: the reader would see a session recording and
// believe a promise had been kept.
func TestCognitoAdoptRefusesAMarkerItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"not a number", "yes"},
		{"a command", "yes\nrm -rf ~\n"},
		{"empty", ""},
		{"whitespace only", "   \n"},
		{"no process", "0"},
		{"a negative process", "-1"},
		{"a pid with a trailer", "1234 /usr/bin/orcli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			c := Cognito{Home: home}

			if err := os.MkdirAll(filepath.Join(home, ".orcli"), 0o700); err != nil {
				t.Fatalf("create the directory: %v", err)
			}
			if err := os.WriteFile(cognitoMarker(home), []byte(tc.body), CognitoFileMode); err != nil {
				t.Fatalf("write the marker: %v", err)
			}

			held, err := c.Adopt()
			if err == nil {
				t.Fatalf("Adopt = true, %v, want an error: %q is not a marker this wrote", held, tc.body)
			}
			if errors.Is(err, ErrCognitoStale) {
				t.Errorf("Adopt = %v, want a fault and not %v", err, ErrCognitoStale)
			}
			if held {
				t.Error("Adopt = true, want false: a marker that cannot be read is not honoured")
			}
		})
	}
}

// TestCognitoAdoptToleratesSurroundingSpace covers a marker carrying
// whitespace around the pid.
//
// Surrounding space is tolerated and nothing else is, so a body a shell added a
// newline to is still read while a body carrying anything beyond whitespace
// around a number is not.
func TestCognitoAdoptToleratesSurroundingSpace(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	if err := os.MkdirAll(filepath.Join(home, ".orcli"), 0o700); err != nil {
		t.Fatalf("create the directory: %v", err)
	}
	body := []byte("\n" + strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(cognitoMarker(home), body, CognitoFileMode); err != nil {
		t.Fatalf("write the marker: %v", err)
	}

	held, err := c.Adopt()
	if err != nil {
		t.Fatalf("Adopt = %v, want nil", err)
	}
	if !held {
		t.Error("Adopt = false, want true: whitespace around the pid is tolerated")
	}
}

// TestCognitoRelease covers turning the mode off.
func TestCognitoRelease(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	if _, err := c.Hold(); err != nil {
		t.Fatalf("Hold = %v, want nil", err)
	}

	removed, err := c.Release()
	if err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}
	if !removed {
		t.Error("Release = false, want true: there was a marker to remove")
	}
}

// TestCognitoReleaseOfNothingIsNotAFault covers turning the mode off twice.
//
// A marker already absent is the outcome arrived at by another route, not a
// fault to report, so a session shutting down twice does not report a failure.
func TestCognitoReleaseOfNothingIsNotAFault(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	removed, err := c.Release()
	if err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}
	if removed {
		t.Error("Release = true, want false: there was no marker")
	}
}

// TestCognitoReleaseRemovesRatherThanEmpties covers why the mode is off after a
// release.
//
// An emptied marker is a file that exists and holds no pid, which Adopt treats
// as a fault rather than as an absence, since the two are indistinguishable to
// whoever reads it next. Leaving it in place would turn every clean shutdown
// into an error for the session that starts next.
func TestCognitoReleaseRemovesRatherThanEmpties(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	if _, err := c.Hold(); err != nil {
		t.Fatalf("Hold = %v, want nil", err)
	}
	if _, err := c.Release(); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}

	if _, err := os.Stat(cognitoMarker(home)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat the marker = %v, want %v: the file should be gone", err, fs.ErrNotExist)
	}

	held, err := c.Adopt()
	if err != nil {
		t.Fatalf("Adopt = %v, want nil", err)
	}
	if held {
		t.Error("Adopt = true, want false: the mode is off after a release")
	}
}

// TestCognitoRefusesAFileAtTheDirectory covers a reader who put a file where
// the directory belongs.
//
// A file at the sessions path is something a reader put there and replacing it
// destroys it, so it is reported rather than created over.
func TestCognitoRefusesAFileAtTheDirectory(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	path := filepath.Join(home, ".orcli")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	if _, err := c.Hold(); err == nil {
		t.Error("Hold = nil, want an error: the path is a file and not a directory")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat the file: %v", err)
	}
}

// TestCognitoLeavesNoTemporaryFile covers what a hold leaves beside the marker.
//
// The bytes go to a temporary file that is renamed over the target, so a
// failure leaves the previous marker rather than a half-written pid that no
// session would recognise. The temporary file itself must be gone, since a
// reader would have to recognise it and nothing else here names it.
func TestCognitoLeavesNoTemporaryFile(t *testing.T) {
	home := t.TempDir()
	c := Cognito{Home: home}

	if _, err := c.Hold(); err != nil {
		t.Fatalf("Hold = %v, want nil", err)
	}

	entries, err := os.ReadDir(filepath.Join(home, ".orcli"))
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".cognito-") {
			t.Errorf("a temporary file was left behind: %q", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries, want 1", len(entries))
	}
}

// TestCognitoHomeDefaultsToTheProcess covers the empty Home.
//
// The field exists so a test does not need a home directory of its own and so
// the caller passes the directory the reader's machine actually has. An empty
// value resolves the process home, which is the ordinary path through the code.
//
// The marker is not written, so nothing is left on the reader's machine. Only
// the path is resolved, because writing to a real home directory is writing to
// the machine running the suite.
func TestCognitoHomeDefaultsToTheProcess(t *testing.T) {
	if _, err := os.UserHomeDir(); err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}

	got, err := (Cognito{}).path()
	if err != nil {
		t.Fatalf("path = %v, want nil", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".orcli", cognitoName)) {
		t.Errorf("path = %q, want it to end in %q", got,
			filepath.Join(".orcli", cognitoName))
	}
}
