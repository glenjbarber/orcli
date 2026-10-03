package saved

// schema is the database a save writes.
//
// The messages table has one nullable column for the turns a tool call makes, rather
// than a column per field. One column carrying an envelope is what leaves room for a
// field added later without migrating the files already written, and it is why role
// and content stay ordinary columns a reader can query with any SQLite tool.
const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
	seq          INTEGER PRIMARY KEY,
	role         TEXT NOT NULL,
	content      TEXT NOT NULL,
	name         TEXT,
	tool_call_id TEXT,
	tool_calls   TEXT
);

CREATE INDEX IF NOT EXISTS messages_role ON messages (role);
`
