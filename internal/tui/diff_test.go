package tui

import (
	"strings"
	"testing"
)

// joined renders the rows DiffRows returned, so a test can check that no text was
// lost or reordered on the way through.
func joined(rows []string) string { return strings.Join(rows, "\n") }

// spanText returns the text a span covers.
func spanText(reply string, s Span) string { return reply[s.Start:s.End] }

// TestDiffRowsKeepsEveryCharacter is the first property, and the one a colouriser
// most easily breaks: recognising a block must not change the text. A reader who
// copies a patch out of the log has to get the patch back.
func TestDiffRowsKeepsEveryCharacter(t *testing.T) {
	const reply = "Here is the change.\n\n" +
		"```diff\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n-old\n+new\n same\n```\n"

	rows, _ := DiffRows(reply)
	if got := joined(rows); got != reply {
		t.Errorf("the text changed:\ngot  %q\nwant %q", got, reply)
	}
}

// TestDiffRowsClassifiesAPatch covers the whole vocabulary: the file header, the
// hunk header, an addition, a removal, and a context line that belongs to neither.
func TestDiffRowsClassifiesAPatch(t *testing.T) {
	const reply = "```diff\n" +
		"--- a/x.go\n" +
		"+++ b/x.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		" context\n" +
		"-removed\n" +
		"+added\n" +
		"```\n"

	rows, spans := DiffRows(reply)

	want := []Role{
		RoleNone,     // the opening fence
		RoleDiffHunk, // --- a/x.go
		RoleDiffHunk, // +++ b/x.go
		RoleDiffHunk, // @@ -1,3 +1,3 @@
		RoleNone,     // context is structure, not a change
		RoleDiffDel,  // -removed
		RoleDiffAdd,  // +added
		RoleNone,     // the closing fence
		RoleNone,     // the trailing empty line the split produced
	}
	got := DiffSpans(rows)

	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d (%q) is role %d, want %d", i, rows[i], got[i], want[i])
		}
	}
	if len(spans) != 5 {
		t.Fatalf("got %d spans, want 5: only the classified rows are spanned", len(spans))
	}
}

// TestDiffSpanOffsetsPointAtTheirRow is what would make a colouriser paint the
// wrong text. A span is a byte range into the reply, and an offset one out lands
// on the line after the one it meant.
func TestDiffSpanOffsetsPointAtTheirRow(t *testing.T) {
	const reply = "text before\n\n```diff\n@@ -1 +1 @@\n-old\n+new\n```\n"

	_, spans := DiffRows(reply)

	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}

	got := make([]string, len(spans))
	for i, s := range spans {
		got[i] = spanText(reply, s)
	}
	for _, want := range []string{"@@ -1 +1 @@", "-old", "+new"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no span covers %q; spans are %q, %q, %q", want, got[0], got[1], got[2])
		}
	}
}

// TestDiffRowsLeavesOrdinaryCodeAlone is the guard against colouring prose. A
// block tagged with any other language is not a patch, and a reader shown a
// diff-tinted shell script would be misled about what changed.
func TestDiffRowsLeavesOrdinaryCodeAlone(t *testing.T) {
	const reply = "```go\nfunc main() {\n\tprintln(\"-not a diff\")\n}\n```\n"

	rows, spans := DiffRows(reply)

	if got := joined(rows); got != reply {
		t.Errorf("the text changed: %q", got)
	}
	if len(spans) != 0 {
		t.Errorf("got %d spans for a go block, want 0", len(spans))
	}
}

// TestDiffRowsFindsAnIndentedFence covers the fence a model writes inside a list.
// Recognising the marker only at the start of a line would miss it, and the block
// would then be folded as ordinary code, which is the failure the fold makes worse.
func TestDiffRowsFindsAnIndentedFence(t *testing.T) {
	const reply = "The change:\n\n  ```diff\n  @@ -1 +1 @@\n  -old\n  +new\n  ```\n"

	rows, spans := DiffRows(reply)

	if got := joined(rows); got != reply {
		t.Errorf("the text changed: %q", got)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3: an indented block is still a block", len(spans))
	}
}

// TestDiffSpansLeaveIndentationUncoloured covers what the indentation is for. A
// patch row the model indented inside a list has spaces that are not part of the
// change, and tinting them as an addition would put four columns of colour on a
// row that added no columns at all.
func TestDiffSpansLeaveIndentationUncoloured(t *testing.T) {
	const reply = "  ```diff\n  @@ -1 +1 @@\n  -old\n  +new\n  ```\n"

	_, spans := DiffRows(reply)

	for _, s := range spans {
		if got := spanText(reply, s); strings.HasPrefix(got, " ") {
			t.Errorf("span covers the indentation: %q", got)
		}
	}
	if got, want := spanText(reply, spans[1]), "-old"; got != want {
		t.Errorf("span is %q, want %q", got, want)
	}
}

// TestIsDiffFenceNamesOnlyDiffs covers the promise check itself, including the
// spacing a model varies and the language a fence may carry.
func TestIsDiffFenceNamesOnlyDiffs(t *testing.T) {
	diffs := []string{"diff", "diff -u", "patch", "DIFF", "  diff  "}
	for _, info := range diffs {
		if !IsDiffFence(info) {
			t.Errorf("IsDiffFence(%q) is false, want true", info)
		}
	}

	others := []string{"", "go", "rust", "diffuse", "console", "json"}
	for _, info := range others {
		if IsDiffFence(info) {
			t.Errorf("IsDiffFence(%q) is true, want false", info)
		}
	}
}

// TestDiffRowRoleIsByFirstByte covers the rule that classification is the patch
// format's own and nothing else. A prose line inside a patch that begins with a
// dash is a removal, because that is what a dash at the start of a patch row is.
func TestDiffRowRoleIsByFirstByte(t *testing.T) {
	cases := []struct {
		row  string
		want Role
	}{
		{"+added", RoleDiffAdd},
		{"-removed", RoleDiffDel},
		{"@@ -1,3 +1,4 @@", RoleDiffHunk},
		{"--- a/x", RoleDiffHunk},
		{"+++ b/x", RoleDiffHunk},
		{" context", RoleNone},
		{"", RoleNone},
		{"+", RoleDiffAdd},
		{"-", RoleDiffDel},
		{"@ not a hunk", RoleNone},
	}
	for _, c := range cases {
		got, _ := diffRowRole(c.row)
		if got != c.want {
			t.Errorf("diffRowRole(%q) is %d, want %d", c.row, got, c.want)
		}
	}
}

// TestDiffRowsHandlesAnUnclosedFence covers a reply that ends inside a block,
// which a stream cut short produces. The text still has to survive intact.
func TestDiffRowsHandlesAnUnclosedFence(t *testing.T) {
	const reply = "```diff\n@@ -1 +1 @@\n-old\n+new"

	rows, spans := DiffRows(reply)

	if got := joined(rows); got != reply {
		t.Errorf("the text changed: %q", got)
	}
	if len(spans) != 3 {
		t.Errorf("got %d spans for an unclosed block, want 3", len(spans))
	}
}

// TestDiffRowsHandlesTwoBlocks covers a reply carrying two patches, since that is
// what a model writes when it fixes two files.
func TestDiffRowsHandlesTwoBlocks(t *testing.T) {
	const reply = "```diff\n@@ -1 +1 @@\n-a\n+b\n```\n" +
		"and the other:\n\n```patch\n@@ -5 +5 @@\n-c\n+d\n```\n"

	rows, spans := DiffRows(reply)

	if got := joined(rows); got != reply {
		t.Errorf("the text changed: %q", got)
	}
	if len(spans) != 6 {
		t.Errorf("got %d spans across two blocks, want 6", len(spans))
	}
}

// TestDiffRowsHandlesTildeFences covers the other fence marker, which a model
// uses when its patch itself contains backticks.
func TestDiffRowsHandlesTildeFences(t *testing.T) {
	const reply = "~~~diff\n@@ -1 +1 @@\n-old\n+new\n~~~\n"

	_, spans := DiffRows(reply)
	if len(spans) != 3 {
		t.Errorf("got %d spans, want 3", len(spans))
	}
}

// TestDiffRowsHandlesARowThatRepeats covers the offset by accumulation rather
// than by searching. A patch naming the same line twice is ordinary, and a
// search for the text would put the second row's span on the first.
func TestDiffRowsHandlesARowThatRepeats(t *testing.T) {
	const reply = "```diff\n@@ -1 +1 @@\n+x\n+y\n@@ -9 +9 @@\n-x\n-y\n```\n"

	_, spans := DiffRows(reply)

	if len(spans) != 6 {
		t.Fatalf("got %d spans, want 6", len(spans))
	}
	if got, want := spanText(reply, spans[0]), "@@ -1 +1 @@"; got != want {
		t.Errorf("first span is %q, want %q", got, want)
	}
	if got, want := spanText(reply, spans[4]), "-x"; got != want {
		t.Errorf("fifth span is %q, want %q", got, want)
	}
}

// TestDiffRowsHandlesEmptyInput is the trivial case, kept because it is the one a
// fold reaches on a reply that was nothing at all.
func TestDiffRowsHandlesEmptyInput(t *testing.T) {
	rows, spans := DiffRows("")

	if len(spans) != 0 {
		t.Errorf("got %d spans, want 0", len(spans))
	}
	if len(rows) != 1 || rows[0] != "" {
		t.Errorf("got %q, want one empty row", rows)
	}
}
