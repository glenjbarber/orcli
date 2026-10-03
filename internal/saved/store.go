package saved

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Store is a directory of saved sessions.
//
// The directory is opened once and does not move for the session. One file per save, so
// a file can be handed to somebody else, queried on its own, or deleted without
// touching anything else.
type Store struct {
	dir string
}

// DefaultDir is where sessions live.
//
// It is beside the configuration rather than in it, since the configuration holds a
// credential and a directory of conversations is not one.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("saved: the home directory could not be determined")
	}
	return filepath.Join(home, ".orcli", "sessions"), nil
}

// Open opens a store at dir, creating it if it is absent.
//
// An existing directory is left exactly as it is. A path that exists and is not a
// directory is reported rather than created over, since a file at the sessions path is
// something a reader put there.
func Open(dir string) (*Store, error) {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("saved: create %s: %w", dir, err)
		}
	case err != nil:
		return nil, fmt.Errorf("saved: read %s: %w", dir, err)
	case !info.IsDir():
		return nil, fmt.Errorf("saved: %s is not a directory", dir)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the directory the store is at.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Path returns the file a named session is stored in.
func (s *Store) Path(name string) (string, error) {
	clean, err := sessionName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dir, clean+".db"), nil
}

// Names returns the sessions in the store, in name order.
//
// The order is settled rather than whatever the filesystem returns, so a list a reader
// reads twice reads the same way twice.
func (s *Store) Names() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("saved: read %s: %w", s.dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".db"))
	}
	return names, nil
}

// Save writes a session, replacing any file already at that name.
//
// The write is whole and goes to a temporary file that is renamed over the target, so a
// reader who loses power mid-save has the old file rather than a truncated one. A
// database written in place can be left unreadable, and a conversation is the one thing
// here that cannot be recovered from anywhere else.
func (s *Store) Save(session Session) (string, error) {
	path, err := s.Path(session.Name)
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(s.dir, ".save-*")
	if err != nil {
		return "", fmt.Errorf("saved: create a temporary file: %w", err)
	}
	tmpName := tmp.Name()
	// The temporary file is removed on every path out of this function, so a failure
	// does not leave a half-written session behind for Names to find.
	defer os.Remove(tmpName)

	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("saved: close the temporary file: %w", err)
	}
	if err := writeInto(tmpName, session); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return "", fmt.Errorf("saved: set the mode: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("saved: write %s: %w", path, err)
	}
	return path, nil
}

// writeInto puts the session into a database at path.
func writeInto(path string, session Session) error {
	d, err := open(path)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := d.Exec(schema); err != nil {
		return fmt.Errorf("saved: create the schema: %w", err)
	}

	// The metadata is written in a fixed order rather than by ranging a map, so a
	// file written twice has the same rows in the same order. A file differing only
	// in row order is a file two sessions disagree about.
	for _, entry := range [][2]string{
		{"version", fmt.Sprint(schemaVersion)},
		{"model", session.Model},
		{"prompt_tokens", fmt.Sprint(session.PromptTokens)},
		{"completion_tokens", fmt.Sprint(session.CompletionTokens)},
		{"total_tokens", fmt.Sprint(session.TotalTokens)},
	} {
		if _, err := d.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)`, entry[0], entry[1]); err != nil {
			return fmt.Errorf("saved: record %s: %w", entry[0], err)
		}
	}

	for i, turn := range session.Turns {
		var envelope any
		if !turn.Empty() {
			// The envelope is only written when there is something in it. An ordinary
			// turn stores no envelope at all rather than an empty one, which is what
			// leaves room for a field added later without migrating the files already
			// written.
			data, err := json.Marshal(turn.ToolCalls)
			if err != nil {
				return fmt.Errorf("saved: encode the tool turns of %d: %w", i, err)
			}
			envelope = string(data)
		}

		if _, err := d.Exec(
			`INSERT INTO messages (seq, role, content, name, tool_call_id, tool_calls)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			i, turn.Role, turn.Content,
			nullIfEmpty(turn.Name), nullIfEmpty(turn.ToolCallID), envelope,
		); err != nil {
			return fmt.Errorf("saved: write turn %d: %w", i, err)
		}
	}
	return nil
}

// nullIfEmpty returns nil for an empty string, so an ordinary turn stores SQL NULL
// rather than an empty string.
//
// A NULL says "this turn has no tool" and an empty string says "this turn has a tool
// with nothing in it", which are different and only one of them is true.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Load reads a session by name.
//
// The file is opened rather than stat-ed first, since a driver reports a missing file in
// its own way and a store that checked first would have to know that way.
//
// A file written by a later client is refused rather than half-read, and a file that
// cannot be opened is reported rather than treated as an absence.
func (s *Store) Load(name string) (Session, error) {
	var session Session

	path, err := s.Path(name)
	if err != nil {
		return session, err
	}

	d, err := open(path)
	if err != nil {
		// A file that is not there is a session the reader has not saved yet, which
		// is a state rather than a fault.
		if isAbsent(err) {
			return session, fmt.Errorf("%w: %s", ErrNoStore, name)
		}
		return session, fmt.Errorf("saved: read %s: %w", path, err)
	}
	defer d.Close()

	version, err := readVersion(d)
	if err != nil {
		return session, err
	}
	if version > schemaVersion {
		return session, describeVersion(version, schemaVersion)
	}
	if version < minimumReadable {
		return session, fmt.Errorf("saved: %s is version %d, which is older than this client reads",
			name, version)
	}

	session.Name = name
	session.Model = readMeta(d, "model")
	session.PromptTokens = readCount(d, "prompt_tokens")
	session.CompletionTokens = readCount(d, "completion_tokens")
	session.TotalTokens = readCount(d, "total_tokens")

	turns, err := readTurns(d, name)
	if err != nil {
		return session, err
	}
	session.Turns = turns
	return session, nil
}

// readTurns reads the messages out of an open database.
func readTurns(d *sql.DB, session string) ([]Turn, error) {
	rows, err := d.Query(
		`SELECT role, content, name, tool_call_id, tool_calls
		 FROM messages ORDER BY seq`)
	if err != nil {
		return nil, fmt.Errorf("saved: read the turns of %s: %w", session, err)
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var (
			turn     Turn
			name     sql.NullString
			callID   sql.NullString
			envelope sql.NullString
		)
		if err := rows.Scan(&turn.Role, &turn.Content, &name, &callID, &envelope); err != nil {
			return nil, fmt.Errorf("saved: read a turn of %s: %w", session, err)
		}
		turn.Name = name.String
		turn.ToolCallID = callID.String

		if envelope.Valid && envelope.String != "" {
			if err := json.Unmarshal([]byte(envelope.String), &turn.ToolCalls); err != nil {
				return nil, fmt.Errorf("saved: the tool turns of %s are not readable: %w",
					session, err)
			}
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("saved: read the turns of %s: %w", session, err)
	}
	return turns, nil
}

// readVersion reads the schema version out of a database.
//
// A file with no version is the first version, since the column was added after the
// first schema and a file written before it is exactly the case this covers.
func readVersion(d *sql.DB) (int, error) {
	var value string
	row := d.QueryRow(`SELECT value FROM meta WHERE key = 'version'`)
	if err := row.Scan(&value); errors.Is(err, sql.ErrNoRows) {
		return minimumReadable, nil
	} else if err != nil {
		return 0, fmt.Errorf("saved: read the version: %w", err)
	}

	var version int
	if _, err := fmt.Sscanf(value, "%d", &version); err != nil {
		return 0, fmt.Errorf("saved: the version is %q, which is not a number", value)
	}
	return version, nil
}

// readMeta reads a string out of the metadata, or empty.
func readMeta(d *sql.DB, key string) string {
	var value string
	if err := d.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value); err != nil {
		return ""
	}
	return value
}

// readCount reads a number out of the metadata, or zero.
//
// A count that will not parse is zero rather than a failure: a session whose counts are
// unreadable is still a session whose conversation is readable, and refusing it would
// lose the conversation over a figure shown in a bar.
func readCount(d *sql.DB, key string) int {
	var value string
	if err := d.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value); err != nil {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
		return 0
	}
	return n
}
