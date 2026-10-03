package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Cognito is the marker recording that the no-recording mode is held.
//
// The mode is recorded by a file beside the configuration rather than by a key
// in it, and the reason is the one that governs every writer of that file: it
// holds the credential and it is rewritten only by its two named writers, so a
// key here would mean a third writer of a credential file. A marker beside it
// records the same fact while holding no credential, so it may be written as
// often as the mode changes and as freely as the configuration may not.
//
// The marker records the process that wrote it. That is what makes a marker a
// crash left behind distinguishable from one a running session holds: the two
// are the same file and the pid is the only difference, so a session finding
// one that is not its own has to be able to tell whether to honour it or
// report it.
//
// It holds no credential and nothing is read from the environment to find it,
// on the same terms as the configuration file. No string bound out of it for a
// diagnostic is filtered, because it holds nothing a filter would be for.
type Cognito struct {
	// Home is the directory the marker is read from and written to.
	//
	// It is a field rather than a name resolved inside this file so the caller
	// passes the directory the reader's machine actually has, and so a test does
	// not need a home directory of its own. An empty value resolves the home
	// directory of the process.
	Home string
}

// cognitoName is the name of the marker inside the orcli directory.
const cognitoName = "cognito"

// CognitoFileMode is the mode the marker must carry.
//
// It is 0600, the same rule that governs the configuration file. The marker
// holds no credential, so the mode is not a leak and the reason for it is
// narrower: it names a process on this host, and a marker any account could
// write is a marker any account could forge, and a forged marker is a mode a
// reader never asked for held over a conversation that does get recorded.
const CognitoFileMode fs.FileMode = 0o600

// ErrCognitoStale is returned when a marker exists and belongs to a process
// that is not this one.
//
// It is a report and not a refusal. The pid is the only thing that tells a
// marker a running session wrote from one a crash left, so a session finding a
// marker that is not its own says so rather than honouring a mode it did not
// ask for and discovering it later. The marker is left exactly as it is and
// the caller decides.
var ErrCognitoStale = errors.New("config: the cognito marker belongs to another process")

// Adopt reads the marker and reports whether the mode is held by this process.
//
// A missing marker is an absence and not a failure, so a session with no mode
// held opens and records as it always did. A marker that exists and cannot be
// read is a fault rather than an absence, since a mode that promised nothing
// would be recorded and cannot then be told apart from one that was not held at
// all: the reader would see a session recording and believe the promise had been
// kept.
//
// A marker held by another process is reported through ErrCognitoStale and is
// not honoured. The caller decides what to do about it, since removing a
// marker belonging to a session that is running is not a decision this file
// makes on the reader's behalf.
func (c Cognito) Adopt() (bool, error) {
	path, err := c.path()
	if err != nil {
		return false, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("config: read the cognito marker: %w", err)
	}

	pid, err := parseCognitoPID(data)
	if err != nil {
		return false, fmt.Errorf("config: read the cognito marker: %w", err)
	}
	if pid != os.Getpid() {
		return false, fmt.Errorf("%w: it names process %d and this one is %d",
			ErrCognitoStale, pid, os.Getpid())
	}
	return true, nil
}

// Hold writes the marker for this process, and reports where it went.
//
// A marker already present is replaced rather than refused, since the mode is a
// mode and turning it on over one a crash left is the ordinary case. The
// replacement is whole: the bytes go to a temporary file that is renamed over
// the target, so a failure part way leaves the previous marker rather than a
// half-written pid that no session would recognise.
func (c Cognito) Hold() (string, error) {
	path, err := c.path()
	if err != nil {
		return "", err
	}
	if err := ensureCognitoDir(filepath.Dir(path)); err != nil {
		return "", err
	}

	// The pid alone, with no key and no trailing newline. A marker is read by
	// parsing a number out of it, and the shortest thing that carries that is
	// the thing least likely to be written by something else and then taken
	// for one of these.
	body := []byte(strconv.Itoa(os.Getpid()))

	tmp, err := os.CreateTemp(filepath.Dir(path), ".cognito-*")
	if err != nil {
		return "", fmt.Errorf("config: create a temporary marker: %w", err)
	}
	tmpName := tmp.Name()
	// The temporary file is removed on every path out of this function, so a
	// failure does not leave a file beside the marker that a reader would have
	// to recognise and this file does not name.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("config: write the cognito marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("config: write the cognito marker: %w", err)
	}
	if err := os.Chmod(tmpName, CognitoFileMode); err != nil {
		return "", fmt.Errorf("config: set the mode on the cognito marker: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("config: write %s: %w", path, err)
	}
	return path, nil
}

// Release removes the marker, and reports whether there was one.
//
// A marker that is not there is not a failure. Turning the mode off is a thing
// the reader asked for, and a marker already absent is that outcome arrived at
// by another route, not a fault to report.
//
// The marker is removed rather than emptied. An emptied marker is a file that
// exists and holds no pid, which the reader above treats as a fault rather than
// as an absence, since the two are indistinguishable to whoever reads it next.
func (c Cognito) Release() (bool, error) {
	path, err := c.path()
	if err != nil {
		return false, err
	}

	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("config: remove the cognito marker: %w", err)
	}
	return true, nil
}

// path is where the marker lives.
func (c Cognito) path() (string, error) {
	home := c.Home
	if home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil || resolved == "" {
			return "", errors.New("config: the home directory could not be determined")
		}
		home = resolved
	}
	return filepath.Join(home, ".orcli", cognitoName), nil
}

// ensureCognitoDir creates the orcli directory when it is absent, and checks
// it when it is there.
//
// A path that exists and is not a directory is reported rather than created
// over, since a file at the sessions path is something a reader put there and
// replacing it destroys it. The directory is 0700 for the reason the marker is
// 0600: it holds the sessions and the marker, and neither is any business of
// another account.
func ensureCognitoDir(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("config: create %s: %w", dir, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("config: read %s: %w", dir, err)
	case !info.IsDir():
		return fmt.Errorf("config: %s is not a directory", dir)
	}
	return nil
}

// parseCognitoPID reads the process out of a marker.
//
// Surrounding space is tolerated and nothing else is. A marker this client wrote
// carries a number and nothing else, so a body that is anything longer than a
// number with whitespace around it is a file someone else wrote, and reading it
// as one of these is how a session starts honouring a mode that was never
// turned on. An unreadable marker is a fault rather than an absence for the same
// reason, and the caller is told which process the marker names only when it
// named one.
func parseCognitoPID(data []byte) (int, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return 0, errors.New("config: the cognito marker is empty")
	}

	pid, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("config: the cognito marker is %q, which is not a process", text)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("config: the cognito marker names process %d, which is not one", pid)
	}
	return pid, nil
}
