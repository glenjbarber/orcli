package config

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// recorder captures what was written to the reader.
type recorder struct {
	out  strings.Builder
	warn []error
	cfg  Config
	// failWrite makes the write fail, so the path where an approval cannot be
	// recorded can be tested.
	failWrite bool
}

// trust returns a Trust wired to an in-memory configuration.
//
// The configuration is held rather than read from a file, so the gate can be
// tested without a home directory and without a file whose mode has to be kept
// at 0600 for a test to pass for the wrong reason.
func trust(answer string, cfg Config) (*recorder, Trust) {
	r := &recorder{cfg: cfg}

	t := Trust{
		In:  strings.NewReader(answer),
		Out: &r.out,
		Warn: func(err error) {
			r.warn = append(r.warn, err)
		},
		Read: func() (Config, bool, error) { return r.cfg, true, nil },
		Write: func(c Config) error {
			if r.failWrite {
				return errors.New("config: the record could not be written")
			}
			r.cfg = c
			return nil
		},
	}
	return r, t
}

// TestTrustIsNotAskedTwice covers a directory already in the record.
func TestTrustIsNotAskedTwice(t *testing.T) {
	dir := t.TempDir()

	// The answer is a refusal, so the test fails if the question is asked.
	r, tr := trust("n\n", Config{Trusted: []string{dir}})

	if !tr.EnsureTrusted(dir) {
		t.Error("EnsureTrusted = false, want true: the directory is already trusted")
	}
	if r.out.Len() != 0 {
		t.Errorf("the question was asked again: %q", r.out.String())
	}
}

// TestTrustRecordsAnApproval covers the ordinary answer.
func TestTrustRecordsAnApproval(t *testing.T) {
	dir := t.TempDir()
	r, tr := trust("y\n", Config{})

	if !tr.EnsureTrusted(dir) {
		t.Fatal("EnsureTrusted = false, want true for an approval")
	}

	if !strings.Contains(r.out.String(), dir) {
		t.Errorf("the question did not name the directory: %q", r.out.String())
	}
	if len(r.cfg.Trusted) != 1 || r.cfg.Trusted[0] != dir {
		t.Errorf("Trusted = %v, want [%s]", r.cfg.Trusted, dir)
	}
}

// TestTrustAcceptsTheFullWord covers the second spelling of yes.
func TestTrustAcceptsTheFullWord(t *testing.T) {
	dir := t.TempDir()
	r, tr := trust("yes\n", Config{})

	if !tr.EnsureTrusted(dir) {
		t.Fatal("EnsureTrusted = false, want true for the full word")
	}
	if len(r.cfg.Trusted) != 1 {
		t.Errorf("Trusted = %v, want one entry", r.cfg.Trusted)
	}
}

// TestTrustIgnoresCaseAndSpace covers the shapes a reader actually types.
func TestTrustIgnoresCaseAndSpace(t *testing.T) {
	for _, answer := range []string{"Y\n", "YES\n", "  y  \n", "Yes\n"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			dir := t.TempDir()
			r, tr := trust(answer, Config{})

			if !tr.EnsureTrusted(dir) {
				t.Errorf("EnsureTrusted = false for the answer %q, want true", answer)
			}
			if len(r.cfg.Trusted) != 1 {
				t.Errorf("Trusted = %v, want one entry", r.cfg.Trusted)
			}
		})
	}
}

// TestTrustRecordsARefusal covers every other answer, which is the same answer.
//
// A reader who did not mean to answer must not approve a program by pressing a
// key they pressed for another reason.
func TestTrustRecordsARefusal(t *testing.T) {
	for _, answer := range []string{"n\n", "no\n", "\n", "y es\n", "1\n", "true\n", "sure\n"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			dir := t.TempDir()
			r, tr := trust(answer, Config{})

			if tr.EnsureTrusted(dir) {
				t.Errorf("EnsureTrusted = true for the answer %q, want false", answer)
			}
			if len(r.cfg.Trusted) != 0 {
				t.Errorf("Trusted = %v, want a refusal to record nothing", r.cfg.Trusted)
			}
		})
	}
}

// TestTrustRefusesWhenThereIsNothingToRead covers a non-interactive reader.
//
// A reader with nothing to answer from is refused rather than waited on, because
// a wait that ends with the session is a session that appears to hang.
func TestTrustRefusesWhenThereIsNothingToRead(t *testing.T) {
	dir := t.TempDir()
	r, tr := trust("", Config{})

	tr.In = nil

	if tr.EnsureTrusted(dir) {
		t.Error("EnsureTrusted = true, want false with no reader")
	}
	if len(r.cfg.Trusted) != 0 {
		t.Errorf("Trusted = %v, want nothing recorded", r.cfg.Trusted)
	}
}

// TestTrustRefusesAnUnwiredGate covers a caller that has not connected the file
// operations.
//
// The answer could not be honored even if it was given, so the directory is not
// trusted and no approval is claimed.
func TestTrustRefusesAnUnwiredGate(t *testing.T) {
	dir := t.TempDir()
	tr := Trust{In: strings.NewReader("y\n"), Out: &strings.Builder{}}

	if tr.EnsureTrusted(dir) {
		t.Error("EnsureTrusted = true, want false when the record cannot be written")
	}
}

// TestTrustKeepsAnApprovalItCannotRecord covers the fault that is not a
// refusal.
//
// The reader approved this directory and the session is already running here.
// A failure to write the record is reported, and the approval still stands for
// this session rather than sending the reader back to the prompt.
func TestTrustKeepsAnApprovalItCannotRecord(t *testing.T) {
	dir := t.TempDir()
	r, tr := trust("y\n", Config{})
	r.failWrite = true

	if !tr.EnsureTrusted(dir) {
		t.Error("EnsureTrusted = false, want true: the reader approved it")
	}
	if len(r.warn) != 1 {
		t.Errorf("reported %d faults, want 1: a failure to record is reported", len(r.warn))
	}
}

// TestTrustReportsAReadFaultWithoutRefusing covers the same distinction on the
// read side.
func TestTrustReportsAReadFaultWithoutRefusing(t *testing.T) {
	dir := t.TempDir()
	r, tr := trust("y\n", Config{})
	tr.Read = func() (Config, bool, error) {
		return Config{}, false, errors.New("config: the file could not be read")
	}

	if !tr.EnsureTrusted(dir) {
		t.Error("EnsureTrusted = false, want true: the reader approved it")
	}
	if len(r.warn) != 1 {
		t.Errorf("reported %d faults, want 1", len(r.warn))
	}
	if len(r.cfg.Trusted) != 1 {
		t.Errorf("Trusted = %v, want the approval recorded", r.cfg.Trusted)
	}
}

// TestTrustAsksOncePerDirectory covers a second session in the same directory.
func TestTrustAsksOncePerDirectory(t *testing.T) {
	dir := t.TempDir()
	r, tr := trust("y\n", Config{})

	if !tr.EnsureTrusted(dir) {
		t.Fatal("EnsureTrusted = false on the first run")
	}
	first := r.out.String()

	// A second call reads the record the first one wrote.
	tr.In = strings.NewReader("n\n")
	if !tr.EnsureTrusted(dir) {
		t.Error("EnsureTrusted = false on the second run, want true: it is recorded")
	}
	if r.out.String() != first {
		t.Errorf("the question was asked twice: %q", r.out.String())
	}
}

// TestTrustKeepsEarlierApprovals covers that recording one does not replace the
// list.
func TestTrustKeepsEarlierApprovals(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	r, tr := trust("y\n", Config{Trusted: []string{first}})
	if !tr.EnsureTrusted(second) {
		t.Fatal("EnsureTrusted = false")
	}

	if len(r.cfg.Trusted) != 2 {
		t.Fatalf("Trusted = %v, want two entries", r.cfg.Trusted)
	}
	if r.cfg.Trusted[0] != first || r.cfg.Trusted[1] != second {
		t.Errorf("Trusted = %v, want the earlier approval kept and the new one added",
			r.cfg.Trusted)
	}
}

// TestTrustMatchesARelativeEntry covers a record written by hand or by an
// earlier version.
//
// A trust list that answers no to a directory it named is worse than one that
// was never written.
func TestTrustMatchesARelativeEntry(t *testing.T) {
	dir := t.TempDir()
	r, tr := trust("n\n", Config{Trusted: []string{".", dir + "/"}})

	if !tr.EnsureTrusted(dir) {
		t.Error("EnsureTrusted = false, want true: the entry names this directory")
	}
	if r.out.Len() != 0 {
		t.Errorf("the question was asked: %q", r.out.String())
	}
}

// TestTrustForHomeNamesTheFile covers the accessor the caller uses to open it
// once rather than reopening per question.
func TestTrustForHomeNamesTheFile(t *testing.T) {
	h := home(t)

	got, err := TrustForHome()
	if err != nil {
		t.Fatalf("TrustForHome: %v", err)
	}
	if want := filepath.Join(h, ".orcli.json"); got != want {
		t.Errorf("path is %q, want %q", got, want)
	}
}

// TestTrustWritesAFileTheNextLoadCanRead closes the loop.
//
// The record is only useful if the next session finds it, so a written approval
// has to come back out of the file rather than out of the gate's memory.
func TestTrustWritesAFileTheNextLoadCanRead(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")
	writeConfig(t, path, `{"OPENROUTER_API_KEY":"sk-or-v1-abc"}`, 0o600)

	dir := t.TempDir()
	var out strings.Builder
	var warns []error

	tr := Trust{
		In:   strings.NewReader("y\n"),
		Out:  &out,
		Warn: func(err error) { warns = append(warns, err) },
		Read: func() (Config, bool, error) {
			cfg, found, err := Load()
			return cfg, found, err
		},
		Write: func(cfg Config) error {
			data, err := marshalIndent(cfg)
			if err != nil {
				return err
			}
			return osWriteFile(path, data)
		},
	}

	if !tr.EnsureTrusted(dir) {
		t.Fatal("EnsureTrusted = false, want true")
	}
	if len(warns) != 0 {
		t.Fatalf("reported %d faults, want none: %v", len(warns), warns)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Trusted) != 1 || cfg.Trusted[0] != dir {
		t.Errorf("Trusted = %v, want [%s]: the record has to survive a reload",
			cfg.Trusted, dir)
	}
}

// TestTrustRecordsKeepTheFileParseable checks the shape the record is written
// in.
//
// The file is meant to be read by hand when something has gone wrong, so it has
// to remain a plain JSON object with the rest of its contents intact.
func TestTrustRecordsKeepTheFileParseable(t *testing.T) {
	h := home(t)
	path := filepath.Join(h, ".orcli.json")
	writeConfig(t, path, `{"OPENROUTER_API_KEY":"sk-or-v1-abc","model":"some/model"}`, 0o600)

	dir := t.TempDir()
	r, tr := trust("y\n", Config{APIKey: "sk-or-v1-abc", Model: "some/model"})

	tr.Write = func(cfg Config) error {
		data, err := marshalIndent(cfg)
		if err != nil {
			return err
		}
		return osWriteFile(path, data)
	}

	if !tr.EnsureTrusted(dir) {
		t.Fatal("EnsureTrusted = false, want true")
	}
	if len(r.warn) != 0 {
		t.Fatalf("reported %d faults, want none", len(r.warn))
	}

	info, err := statFile(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != fs.FileMode(0o600) {
		t.Errorf("the file is now %04o, want 0600: a credential must not go loose", got)
	}
}
