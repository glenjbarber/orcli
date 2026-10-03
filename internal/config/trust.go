package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Trust answers whether a session may run in a directory, and records the
// answer.
//
// A directory is trusted by being listed in the configuration. Trust is asked
// once, on the first run in a directory, and an approval is written to the file
// so it is not asked again. It is recorded there rather than in a marker file
// because it is a property of the configuration and nothing else: the file that
// holds the credential is also the file that holds the record of what that
// credential was allowed to do.
//
// The answer is only y or yes. Everything else is a refusal, and records
// nothing: an empty line from a reader who pressed return without reading, a
// read error, a non-interactive reader, and a key pressed for another reason
// are all the same answer, which is no.
//
// Refusing is not a failure state. A reader who does not want to approve a
// directory can still run the interface; it simply has no tools, since a tool
// acts on the reader's behalf and this directory is not the reader's.
type Trust struct {
	// In is where the answer is read. A caller passes its own reader so the
	// prompt is answered on the goroutine that owns the terminal.
	In io.Reader

	// Out is where the question is written.
	Out io.Writer

	// Warn receives a note about a path that could not be written. It is
	// separate from Out because a failure to record is a thing to report to a
	// reader rather than something to draw in a pane they are about to leave.
	Warn func(error)

	// Read and Write are the file operations. They are fields so the trust
	// gate can be tested without a home directory, and so the caller can
	// record the approval through its own already-open configuration file
	// rather than by reopening it here.
	Read  func() (Config, bool, error)
	Write func(Config) error
}

// EnsureTrusted asks about the directory, and records the answer if it was yes.
//
// It reports whether the directory is trusted. A directory already in the file
// is not asked about again, and a directory that cannot be recorded is reported
// rather than refused: a reader who approved it has approved it, and a failure
// to write the record is a fault worth naming rather than a reason to send them
// back to the prompt.
//
// The question is only asked when it can be answered. A reader with nothing to
// read the answer from is refused rather than waited on, because a wait that
// ends with the session is a session that appears to hang.
func (t Trust) EnsureTrusted(dir string) bool {
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.warn(fmt.Errorf("config: resolve %s: %w", dir, err))
		return false
	}

	if t.Read == nil || t.Write == nil {
		// Nothing to read the record from and nothing to write it to, so the
		// answer could not be honored even if it was given. A caller that has
		// not provided them has not wired up the gate at all.
		return false
	}

	cfg, _, err := t.Read()
	if err != nil && !errors.Is(err, ErrNoAPIKey) {
		// The record could not be read. This is not fatal here: the question
		// below is still worth asking, and an approval that cannot be written
		// is reported rather than treated as a refusal.
		t.warn(err)
	}

	if trusted(cfg.Trusted, dir) {
		return true
	}

	if !t.ask(dir) {
		return false
	}

	cfg.Trusted = append(cfg.Trusted, dir)
	if err := t.Write(cfg); err != nil {
		t.warn(err)
		// The reader approved this directory. Failing to record it is a fault
		// to report, not a reason to treat the approval as though it had not
		// been given, since the session is already running here.
		return true
	}
	return true
}

// ask puts the question, and reads the answer.
func (t Trust) ask(dir string) bool {
	if t.In == nil || t.Out == nil {
		return false
	}

	// The directory is quoted rather than printed bare. A path is a thing a
	// reader is about to agree to, and a path with a space or a control
	// character in it is one they would not recognize in a prompt.
	fmt.Fprintf(t.Out, "orcli wants to run in %s, and may run programs there.\n", dir)
	fmt.Fprint(t.Out, "Trust this directory? [y/N] ")

	line, err := bufio.NewReader(t.In).ReadString('\n')
	if err != nil && line == "" {
		// Nothing was read. A reader with nothing to answer from is refused
		// rather than waited on, and the partial line is refused with it.
		fmt.Fprintln(t.Out)
		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// trusted reports whether dir is already in the list.
//
// The comparison is on the cleaned absolute path. A list written by an earlier
// version, or by hand, may carry a trailing separator or a relative prefix, and
// a trust list that answers no to a directory it named is worse than one that
// was never written.
func trusted(list []string, dir string) bool {
	for _, entry := range list {
		if entry == dir {
			return true
		}
		if abs, err := filepath.Abs(entry); err == nil && abs == dir {
			return true
		}
	}
	return false
}

// warn reports a fault without turning it into a refusal.
func (t Trust) warn(err error) {
	if err == nil || t.Warn == nil {
		return
	}
	t.Warn(err)
}

// TrustForHome is the configuration path used for the trust record.
//
// It is exposed so the caller can open the file once and pass Read and Write,
// rather than having this package reopen it for every question.
func TrustForHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("config: the home directory could not be determined")
	}
	return filepath.Join(home, ".orcli.json"), nil
}
