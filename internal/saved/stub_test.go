package saved

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
)

// stubDriver is an in-memory database/sql driver, so the store can be tested without
// SQLite being installed and without this package depending on it in its tests.
//
// It is a stub rather than a real database on purpose: what the tests check is the
// store's own behavior, which schema it writes, what it puts in each column, and what
// it refuses to read. Checking those against a real SQLite would be testing SQLite.
//
// The driver records the statements and the arguments it was given, so a test can
// assert on the shape of the schema and on what was written into the envelope column,
// which is the part of the format a reader is meant to be able to query for themselves.
type stubDriver struct {
	mu sync.Mutex

	// statements and their arguments, in the order they were executed.
	statements []string
	args       [][]driver.Value

	// rows are what a query on the messages table returns, in order.
	rows [][]driver.Value

	// meta is what a query on the metadata table answers from.
	meta map[string]string

	// missing reports a path as absent when it is opened, which is how the stub
	// stands in for a file that is not there.
	missing map[string]bool

	// failOpen makes every open fail, for the paths where a file exists and cannot be
	// read.
	failOpen bool
}

var stub stubDriver

func init() {
	sql.Register("stub", &stub)
}

// reset clears what the stub has recorded, between tests.
func reset() {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.statements = nil
	stub.args = nil
	stub.rows = nil
	stub.meta = map[string]string{}
	stub.missing = map[string]bool{}
	stub.failOpen = false
}

// recorded returns the statements that mentioned a fragment, with their arguments.
func recorded(fragment string) []recordedStatement {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	var out []recordedStatement
	for i, s := range stub.statements {
		if strings.Contains(s, fragment) {
			out = append(out, recordedStatement{query: s, args: stub.args[i]})
		}
	}
	return out
}

// recordedStatement is one statement and what it was given.
type recordedStatement struct {
	query string
	args  []driver.Value
}

// values renders the arguments, so an assertion can be on what was written rather than
// on how the driver was called.
func (r recordedStatement) values() []string {
	out := make([]string, 0, len(r.args))
	for _, a := range r.args {
		out = append(out, renderValue(a))
	}
	return out
}

// renderValue renders one argument for an assertion.
func renderValue(a driver.Value) string {
	switch t := a.(type) {
	case nil:
		return "<nil>"
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

func (d *stubDriver) Open(name string) (driver.Conn, error) {
	d.mu.Lock()
	missing := d.missing[name]
	fail := d.failOpen
	d.mu.Unlock()

	if missing {
		// The same wording a real driver uses for a file that is not there, so the
		// store cannot tell this from a read failure and has to handle both.
		return nil, errors.New("no such file")
	}
	if fail {
		return nil, errors.New("the file could not be opened")
	}
	return &stubConn{driver: d}, nil
}

// stubConn is one connection to the stub.
type stubConn struct{ driver *stubDriver }

func (c *stubConn) Prepare(query string) (driver.Stmt, error) {
	return &stubStmt{conn: c, query: query}, nil
}

func (c *stubConn) Close() error              { return nil }
func (c *stubConn) Begin() (driver.Tx, error) { return stubTx{}, nil }

// stubTx is a transaction that commits unconditionally, since the stub keeps nothing to
// commit.
type stubTx struct{}

func (stubTx) Commit() error   { return nil }
func (stubTx) Rollback() error { return nil }

// stubStmt is one prepared statement.
type stubStmt struct {
	conn  *stubConn
	query string
}

func (s *stubStmt) Close() error  { return nil }
func (s *stubStmt) NumInput() int { return -1 }

func (s *stubStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.record(args)
	return driver.RowsAffected(1), nil
}

func (s *stubStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.record(args)

	s.conn.driver.mu.Lock()
	rows := s.conn.driver.rows
	meta := s.conn.driver.meta
	s.conn.driver.mu.Unlock()

	switch {
	case strings.Contains(s.query, "FROM meta"):
		// A metadata query selects one column, whatever it asks for, so the
		// column count has to follow the query rather than be fixed. A driver that
		// reported six columns for a one-column query would be refused by
		// database/sql before the store saw a row.
		return &stubRows{columns: []string{"value"}, values: metaRows(meta, s.query, args)}, nil
	case strings.Contains(s.query, "FROM messages"):
		return &stubRows{columns: messageColumns, values: rows}, nil
	default:
		return &stubRows{columns: []string{"value"}}, nil
	}
}

// record notes that a statement was run and what it was given.
func (s *stubStmt) record(args []driver.Value) {
	s.conn.driver.mu.Lock()
	s.conn.driver.statements = append(s.conn.driver.statements, s.query)
	s.conn.driver.args = append(s.conn.driver.args, args)
	s.conn.driver.mu.Unlock()
}

// messageColumns are the columns a query on the messages table selects.
var messageColumns = []string{"role", "content", "name", "tool_call_id", "tool_calls"}

// metaOrder is the order the store writes its metadata in, so a query wanting every row
// answers in a settled order rather than a map iteration.
var metaOrder = []string{"version", "model", "prompt_tokens", "completion_tokens", "total_tokens"}

// metaRows answers a metadata query from the map.
//
// A parameterised query carries its key in the arguments rather than in the text, since
// the text is all placeholders by the time the driver sees it. So the key is read from
// the arguments, and only a query naming its key in single quotes is read from the text.
func metaRows(meta map[string]string, query string, args []driver.Value) [][]driver.Value {
	for _, a := range args {
		if key, ok := a.(string); ok {
			if value, present := meta[key]; present {
				return [][]driver.Value{{value}}
			}
			return nil
		}
	}

	if key, ok := literalKey(query); ok {
		if value, present := meta[key]; present {
			return [][]driver.Value{{value}}
		}
		return nil
	}

	// A query naming no key wants every row, in the order the store writes them.
	var out [][]driver.Value
	for _, key := range metaOrder {
		if value, present := meta[key]; present {
			out = append(out, []driver.Value{value})
		}
	}
	return out
}

// literalKey reads the key out of a query that names one in single quotes.
func literalKey(query string) (string, bool) {
	const open = "'"
	start := strings.Index(query, open)
	if start < 0 {
		return "", false
	}
	rest := query[start+1:]
	end := strings.Index(rest, open)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// stubRows is a result set held in memory.
type stubRows struct {
	columns []string
	values  [][]driver.Value
	at      int
}

func (r *stubRows) Columns() []string { return r.columns }
func (r *stubRows) Close() error      { return nil }

func (r *stubRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}
