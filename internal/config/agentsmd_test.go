package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentsCacheFreshRead covers the first read of a file the cache has never seen:
// it is read from disk and returned.
func TestAgentsCacheFreshRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := NewAgentsCache()
	text, ok := c.Read(path)
	if !ok {
		t.Fatalf("Read: ok = false, want true")
	}
	if text != "first" {
		t.Errorf("text = %q, want %q", text, "first")
	}
}

// TestAgentsCacheHitOnUnchangedModTime covers the cache's reason for existing: a
// second Read of a file whose modification time has not moved returns the cached text
// even though the file on disk now holds something else, proving the file was not
// read again.
func TestAgentsCacheHitOnUnchangedModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := NewAgentsCache()
	if text, ok := c.Read(path); !ok || text != "first" {
		t.Fatalf("first Read = (%q, %v), want (%q, true)", text, ok, "first")
	}

	// Change the file's contents without changing its modification time, so a Read
	// that honors mtime returns the old text and a Read that does not would return
	// the new one.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	text, ok := c.Read(path)
	if !ok {
		t.Fatalf("second Read: ok = false, want true")
	}
	if text != "first" {
		t.Errorf("second Read = %q, want the cached %q (mtime unchanged)", text, "first")
	}
}

// TestAgentsCacheRefreshesOnNewerModTime covers the other side of the same test: once
// the modification time does move forward, the file is read again and both the text
// and the recorded time are replaced.
func TestAgentsCacheRefreshesOnNewerModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := NewAgentsCache()
	if text, ok := c.Read(path); !ok || text != "first" {
		t.Fatalf("first Read = (%q, %v), want (%q, true)", text, ok, "first")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	newer := info.ModTime().Add(time.Second)
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, newer, newer); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	text, ok := c.Read(path)
	if !ok {
		t.Fatalf("second Read: ok = false, want true")
	}
	if text != "second" {
		t.Errorf("second Read = %q, want the refreshed %q (mtime moved forward)", text, "second")
	}
}

// TestAgentsCacheFallsBackOnDeletedFile covers that a file deleted after a successful
// read still answers with the cached text rather than an error or an empty result.
func TestAgentsCacheFallsBackOnDeletedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := NewAgentsCache()
	if text, ok := c.Read(path); !ok || text != "first" {
		t.Fatalf("first Read = (%q, %v), want (%q, true)", text, ok, "first")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	text, ok := c.Read(path)
	if !ok {
		t.Fatalf("Read after delete: ok = false, want true (fall back to cached)")
	}
	if text != "first" {
		t.Errorf("Read after delete = %q, want the cached %q", text, "first")
	}
}

// TestAgentsCacheFallsBackOnUnreadableFile covers a file that still exists and still
// stats, but cannot be read - here, because it has been replaced by a directory of the
// same name, which os.ReadFile refuses. The cache falls back to the cached text rather
// than erroring or clearing what it knows.
func TestAgentsCacheFallsBackOnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := NewAgentsCache()
	if text, ok := c.Read(path); !ok || text != "first" {
		t.Fatalf("first Read = (%q, %v), want (%q, true)", text, ok, "first")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	text, ok := c.Read(path)
	if !ok {
		t.Fatalf("Read after replacement with a directory: ok = false, want true (fall back to cached)")
	}
	if text != "first" {
		t.Errorf("Read after replacement with a directory = %q, want the cached %q", text, "first")
	}
}

// TestAgentsCacheFallsBackOnBackwardsModTime covers a modification time that moves
// backwards, such as a restore from backup: the cache treats it the same as an
// unchanged time, reusing what it has rather than re-reading or erroring.
func TestAgentsCacheFallsBackOnBackwardsModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := NewAgentsCache()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if text, ok := c.Read(path); !ok || text != "first" {
		t.Fatalf("first Read = (%q, %v), want (%q, true)", text, ok, "first")
	}

	older := info.ModTime().Add(-time.Hour)
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, older, older); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	text, ok := c.Read(path)
	if !ok {
		t.Fatalf("Read with a backwards mtime: ok = false, want true")
	}
	if text != "first" {
		t.Errorf("Read with a backwards mtime = %q, want the cached %q, not the file's new %q", text, "first", "second")
	}
}

// TestAgentsCacheNoFallbackBeforeAnySuccess covers the one case the cache has nothing
// to give back: a path that has never been read successfully reports ok = false,
// exactly like os.ReadFile failing outright, rather than fabricating an empty cache
// hit.
func TestAgentsCacheNoFallbackBeforeAnySuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	c := NewAgentsCache()
	text, ok := c.Read(path)
	if ok {
		t.Fatalf("Read of a file that never existed: ok = true, want false")
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
}

// TestAgentsCacheKeyedBySymlinkResolvedPath covers the cache key: the same underlying
// file reached through two different paths - one direct, one through a symlinked
// directory - hits the same entry, so a change seen through one path is reflected
// through the other without a second disk read.
func TestAgentsCacheKeyedBySymlinkResolvedPath(t *testing.T) {
	real := t.TempDir()
	path := filepath.Join(real, "AGENTS.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	linkDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, linkDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaLink := filepath.Join(linkDir, "AGENTS.md")

	c := NewAgentsCache()
	if text, ok := c.Read(path); !ok || text != "first" {
		t.Fatalf("direct Read = (%q, %v), want (%q, true)", text, ok, "first")
	}

	// Delete the file reachable only through the direct path - the symlink target no
	// longer exists either, since it is the same file - and confirm the link path
	// still answers from the same cached entry.
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	text, ok := c.Read(viaLink)
	if !ok {
		t.Fatalf("Read via symlink after delete: ok = false, want true (same cache entry as the direct path)")
	}
	if text != "first" {
		t.Errorf("Read via symlink after delete = %q, want the cached %q", text, "first")
	}
}
