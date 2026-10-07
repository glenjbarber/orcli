package config

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AgentsCache holds the text of one or more AGENTS.md files in memory, each keyed by
// its own absolute, symlink-resolved path, so the same underlying file is recognized
// as the same entry regardless of the working directory or symlink a caller reached it
// through.
//
// The cache exists because AGENTS.md is read once per turn (see cmd/orcli/ask.go,
// introduction), and a file that has not changed since the last turn does not need to
// be read from disk again. The freshness test is the file's own modification time,
// not a hash or a size: a reader editing AGENTS.md mid-session expects the next turn
// to see the edit, and mtime is the cheap, ordinary signal for that, the same one a
// build tool or an editor relies on. A read that found the modification time no newer
// than what is cached returns the cached text without touching the file again, and a
// read that found it newer replaces both the cached text and the recorded time. "Read
// again" means exactly that: the file's Markdown text is sourced again, never executed
// as code, the same as the first read.
//
// The cache is in-memory only, for the lifetime of the AgentsCache value, which is
// ordinarily the lifetime of the process. Nothing here is written to disk, and a new
// AgentsCache starts empty: there is no warm start across runs, on the same grounds
// Config has no warm start of its own environment variable - a cross-run cache would
// need its own invalidation story and that is a different feature from the one asked
// for here.
//
// Three failure cases are folded into one answer, "use what is already cached", rather
// than three separate faults, because a transient disk problem should not cost a
// session its introduction:
//
//   - the file has been deleted since it was last read;
//   - the file exists but cannot be read (permissions, a directory in its place, and
//     so on);
//   - the file's modification time has moved backwards (a restore from backup, a clock
//     changed, a checkout of an older commit).
//
// Each of those is reported to the cache as "not a fresh read", and the cache answers
// with whatever it already holds for that path. A path that has never been read
// successfully has nothing to fall back to, and that case is reported rather than
// papered over: see AgentsCache.Read.
type AgentsCache struct {
	mu      sync.Mutex
	entries map[string]agentsCacheEntry
}

// agentsCacheEntry is one cached file: its text as last read, and the modification
// time that text was read at.
type agentsCacheEntry struct {
	text    string
	modTime time.Time
}

// NewAgentsCache returns an empty cache, ready for Read.
func NewAgentsCache() *AgentsCache {
	return &AgentsCache{entries: make(map[string]agentsCacheEntry)}
}

// Read returns the text of the AGENTS.md (or any other file) named by path, reusing
// the cached text when the file's modification time has not moved forward since the
// last read that succeeded, and reading the file again when it has.
//
// path need not be absolute or free of symlinks; Read resolves it to the same key
// agentsCacheKey would produce for any other path naming the same file, so a caller in
// a different working directory, or one that reached the file through a symlink, still
// hits the same entry.
//
// The boolean result reports whether text came from a successful read, this one or an
// earlier one: it is true on a fresh read and on a cache hit, and false only when the
// file could not be read this time and nothing was ever cached for it either, which is
// the one case Read has nothing to give back. A caller that gets false has the answer
// introduction already gives a missing file: no text, send no message.
func (c *AgentsCache) Read(path string) (string, bool) {
	key := agentsCacheKey(path)

	info, statErr := os.Stat(path)

	c.mu.Lock()
	defer c.mu.Unlock()

	cached, hasCached := c.entries[key]

	if statErr != nil {
		// Deleted or otherwise unreachable: fall back to whatever is already known
		// about this path, rather than treating a stat failure as if the file had
		// never been read.
		return cached.text, hasCached
	}

	modTime := info.ModTime()
	if hasCached && !modTime.After(cached.modTime) {
		// Unchanged, or moved backwards: reuse the cached text either way. A
		// modification time that goes backwards is not a signal that the file is
		// somehow older or less trustworthy than what is cached; it is more often a
		// restore or a checkout, and the cached text is at least as likely to be
		// right as a fresh read would be, so neither counts as a reason to re-read.
		return cached.text, true
	}

	text, err := os.ReadFile(path)
	if err != nil {
		// The stat above succeeded but the read did not: a permissions change or a
		// race with something else removing the file between the two calls. Same
		// fallback as a failed stat.
		return cached.text, hasCached
	}

	c.entries[key] = agentsCacheEntry{text: string(text), modTime: modTime}
	return string(text), true
}

// agentsCacheKey returns path in absolute, symlink-resolved form, so that two
// different strings naming the same file - one reached through a symlink, one not, or
// one relative to a different working directory - land on the same cache entry.
//
// filepath.EvalSymlinks fails outright on a path that does not exist, which is the
// ordinary shape of the case this cache most needs to handle correctly: a file that
// existed, was read and cached, and has since been deleted. Resolving the key from
// scratch on every call, rather than remembering it from the first successful read,
// means a deleted file still resolves to the same key its parent directory would
// produce, which is the key it was cached under while it still existed - as long as
// the file's own name was not itself a symlink, which AGENTS.md ordinarily is not.
// What exists of the path is followed as far as it goes, and the rest is appended
// unresolved.
func agentsCacheKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)

	if linked, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(linked)
	}

	rest := ""
	current := abs
	for {
		if linked, err := filepath.EvalSymlinks(current); err == nil {
			if rest == "" {
				return filepath.Clean(linked)
			}
			return filepath.Clean(filepath.Join(linked, rest))
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Nothing in the path exists at all; there is nowhere left to walk, and
			// the cleaned absolute path is the best key available.
			return abs
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}
