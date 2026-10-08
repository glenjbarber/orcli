package tui

import "testing"

// spanText is defined in diff_test.go and reused here.

// findRole returns the first span with role, or ok false if none exists.
func findRole(spans []Span, role Role) (Span, bool) {
	for _, sp := range spans {
		if sp.Role == role {
			return sp, true
		}
	}
	return Span{}, false
}

// TestParseMarkdownSpansAnATXHeading covers the plain case: a line opening
// with one to six '#' and a space reads as a heading, past the marker.
func TestParseMarkdownSpansAnATXHeading(t *testing.T) {
	text := "## Section two\nbody"
	spans := ParseMarkdown(text)
	sp, ok := findRole(spans, RoleHeading)
	if !ok {
		t.Fatalf("no heading span in %v", spans)
	}
	if got, want := spanText(text, sp), "Section two"; got != want {
		t.Errorf("heading span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownDoesNotHeadASevenHashRun is the ATX boundary: seven or
// more '#' is not a heading, so a line of rule characters is not mistaken
// for one.
func TestParseMarkdownDoesNotHeadASevenHashRun(t *testing.T) {
	text := "####### not a heading"
	spans := ParseMarkdown(text)
	if _, ok := findRole(spans, RoleHeading); ok {
		t.Errorf("a run of seven '#' was read as a heading: %v", spans)
	}
}

// TestParseMarkdownSpansAnUnorderedListMarker covers the bullet case: only
// the marker itself takes RoleList, not the item's own text.
func TestParseMarkdownSpansAnUnorderedListMarker(t *testing.T) {
	text := "- first item"
	spans := ParseMarkdown(text)
	sp, ok := findRole(spans, RoleList)
	if !ok {
		t.Fatalf("no list span in %v", spans)
	}
	if got, want := spanText(text, sp), "-"; got != want {
		t.Errorf("list span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownSpansAnOrderedListMarker covers "1. " rather than a
// bullet character, which is a separate shape listMarkerLen has to parse on
// its own path.
func TestParseMarkdownSpansAnOrderedListMarker(t *testing.T) {
	text := "12. twelfth item"
	spans := ParseMarkdown(text)
	sp, ok := findRole(spans, RoleList)
	if !ok {
		t.Fatalf("no list span in %v", spans)
	}
	if got, want := spanText(text, sp), "12."; got != want {
		t.Errorf("list span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownSpansABlockquoteLine covers '>' at the start of a line,
// which reads as a quote across the whole line rather than only the marker.
func TestParseMarkdownSpansABlockquoteLine(t *testing.T) {
	text := "> a quoted sentence"
	spans := ParseMarkdown(text)
	sp, ok := findRole(spans, RoleQuote)
	if !ok {
		t.Fatalf("no quote span in %v", spans)
	}
	if got, want := spanText(text, sp), text; got != want {
		t.Errorf("quote span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownSpansInlineCode covers a single-line `code` run, which
// must not swallow the rest of the reply the way an unterminated fence
// would.
func TestParseMarkdownSpansInlineCode(t *testing.T) {
	text := "run `go test ./...` before you push"
	spans := ParseMarkdown(text)
	sp, ok := findRole(spans, RoleCode)
	if !ok {
		t.Fatalf("no code span in %v", spans)
	}
	if got, want := spanText(text, sp), "`go test ./...`"; got != want {
		t.Errorf("code span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownSpansAFencedCodeBlockAcrossLines is the case inline code
// must not be mistaken for: a fence opened on one line stays open until a
// closing fence several lines later, and every line between is code even
// though none of them carries a backtick pair of its own.
func TestParseMarkdownSpansAFencedCodeBlockAcrossLines(t *testing.T) {
	text := "before\n```\nline one\nline two\n```\nafter"
	spans := ParseMarkdown(text)

	var covered int
	for _, sp := range spans {
		if sp.Role != RoleCode {
			continue
		}
		covered += sp.End - sp.Start
	}
	// "```" + "line one" + "line two" + "```" = 3 + 8 + 8 + 3.
	if want := len("```") + len("line one") + len("line two") + len("```"); covered != want {
		t.Errorf("fenced block covered %d bytes of code, want %d (spans %v)", covered, want, spans)
	}

	for _, sp := range spans {
		if sp.Role == RoleCode {
			if got := spanText(text, sp); got == "before" || got == "after" {
				t.Errorf("text outside the fence was read as code: %q", got)
			}
		}
	}
}

// TestParseMarkdownSpansALinkURLNotItsText covers the judgment call recorded
// in ParseMarkdown's doc comment: RoleLink lands on the URL inside the
// parens, not on the bracketed text in front of it.
func TestParseMarkdownSpansALinkURLNotItsText(t *testing.T) {
	text := "see [the docs](https://example.com/path) for more"
	spans := ParseMarkdown(text)
	sp, ok := findRole(spans, RoleLink)
	if !ok {
		t.Fatalf("no link span in %v", spans)
	}
	if got, want := spanText(text, sp), "https://example.com/path"; got != want {
		t.Errorf("link span covers %q, want %q", got, want)
	}
	for _, other := range spans {
		if other.Role == RoleLink && other != sp {
			t.Errorf("more than one link span: %v", spans)
		}
	}
}

// TestParseMarkdownSpansBoldAsEmphasis covers the other judgment call:
// bold maps onto the existing RoleEmphasis rather than a role of its own.
func TestParseMarkdownSpansBoldAsEmphasis(t *testing.T) {
	text := "this is **important** right here"
	spans := ParseMarkdown(text)
	sp, ok := findRole(spans, RoleEmphasis)
	if !ok {
		t.Fatalf("no emphasis span in %v", spans)
	}
	if got, want := spanText(text, sp), "**important**"; got != want {
		t.Errorf("emphasis span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownSpansInlineCodeInsideAListItem is the nested case: a list
// item's marker takes RoleList and the inline code inside its text still
// takes RoleCode, as two separate spans over the same line rather than one
// overwriting the other.
func TestParseMarkdownSpansInlineCodeInsideAListItem(t *testing.T) {
	text := "- run `make test` first"
	spans := ParseMarkdown(text)

	listSp, ok := findRole(spans, RoleList)
	if !ok {
		t.Fatalf("no list span in %v", spans)
	}
	if got, want := spanText(text, listSp), "-"; got != want {
		t.Errorf("list span covers %q, want %q", got, want)
	}

	codeSp, ok := findRole(spans, RoleCode)
	if !ok {
		t.Fatalf("no code span in %v", spans)
	}
	if got, want := spanText(text, codeSp), "`make test`"; got != want {
		t.Errorf("code span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownSurvivesFoldingAcrossPhysicalLines is the integration
// case: a reply with markdown on several of its own lines is folded by
// foldRowLines (stack.go) into one physical row per line break, and each
// physical row still carries only the spans that belong to it, rebased to
// start at zero - the same guarantee foldRowLines already gives a plain
// Span, now exercised with the spans ParseMarkdown actually produces.
func TestParseMarkdownSurvivesFoldingAcrossPhysicalLines(t *testing.T) {
	text := "# Heading\n- item with `code`\n> a quote"
	row := Row{Kind: KindReply, Text: text, Spans: ParseMarkdown(text)}

	lines := foldRowLines(row, 0)
	if len(lines) != 3 {
		t.Fatalf("got %d folded lines, want 3: %v", len(lines), lines)
	}

	headingSp, ok := findRole(lines[0].Spans, RoleHeading)
	if !ok {
		t.Fatalf("line 0 (%q) carries no heading span: %v", lines[0].Text, lines[0].Spans)
	}
	if got, want := spanText(lines[0].Text, headingSp), "Heading"; got != want {
		t.Errorf("line 0 heading span covers %q, want %q", got, want)
	}

	listSp, ok := findRole(lines[1].Spans, RoleList)
	if !ok {
		t.Fatalf("line 1 (%q) carries no list span: %v", lines[1].Text, lines[1].Spans)
	}
	if got, want := spanText(lines[1].Text, listSp), "-"; got != want {
		t.Errorf("line 1 list span covers %q, want %q", got, want)
	}
	codeSp, ok := findRole(lines[1].Spans, RoleCode)
	if !ok {
		t.Fatalf("line 1 (%q) carries no code span: %v", lines[1].Text, lines[1].Spans)
	}
	if got, want := spanText(lines[1].Text, codeSp), "`code`"; got != want {
		t.Errorf("line 1 code span covers %q, want %q", got, want)
	}

	quoteSp, ok := findRole(lines[2].Spans, RoleQuote)
	if !ok {
		t.Fatalf("line 2 (%q) carries no quote span: %v", lines[2].Text, lines[2].Spans)
	}
	if got, want := spanText(lines[2].Text, quoteSp), lines[2].Text; got != want {
		t.Errorf("line 2 quote span covers %q, want %q", got, want)
	}
}

// TestDeliverAttachesMarkdownSpans is the wiring test: Session.Deliver, not
// just ParseMarkdown on its own, is what a caller like ask.go actually
// reaches, so this confirms the row Deliver appends carries the spans
// ParseMarkdown would have produced from the same text.
func TestDeliverAttachesMarkdownSpans(t *testing.T) {
	s := New(Options{})
	s.Deliver("# Title\nplain line", 0)

	rows := s.Log().Rows()
	if len(rows) == 0 {
		t.Fatal("Deliver appended no row")
	}
	row := rows[len(rows)-1]
	if _, ok := findRole(row.Spans, RoleHeading); !ok {
		t.Errorf("delivered row carries no heading span: %v", row.Spans)
	}
}

// TestParseMarkdownSpansEveryRoleAcrossAMultiParagraphReply is the
// multi-paragraph case the task asked for directly: a heading, a quote, a
// list item with inline code, and a link, each in its own paragraph, each
// read correctly out of the one AST goldmark builds for the whole reply
// rather than guessed line by line.
func TestParseMarkdownSpansEveryRoleAcrossAMultiParagraphReply(t *testing.T) {
	text := "# Plan\n\n> worth remembering\n\n- run `go test ./...`\n\nsee [the docs](https://example.com) for more"
	spans := ParseMarkdown(text)

	for _, role := range []Role{RoleHeading, RoleQuote, RoleList, RoleCode, RoleLink} {
		if _, ok := findRole(spans, role); !ok {
			t.Errorf("role %s never fired on %q: %v", RoleName(role), text, spans)
		}
	}
}

// TestParseMarkdownGetsLazyContinuationRightWhereALineScannerCannot is the
// case a line-by-line scanner cannot get right by construction: a list
// item's paragraph can continue onto a line with no marker of its own
// (CommonMark's "lazy continuation"), and that continuation line is still
// part of the list item's own paragraph, not a plain line sitting after it.
// A parser that decides what a line is from that line's own leading bytes
// has nothing on the second line that says "list item"; a real AST knows
// because it tracked the block structure across both lines while parsing.
//
// This does not assert a span lands on the continuation line itself (there
// is nothing in it for RoleList, RoleCode etc. to colour) - it asserts the
// one thing the old scanner would have gotten wrong if it had tried: the
// list marker is still found on the first line, undisturbed by the
// continuation line that follows it with no marker of its own.
func TestParseMarkdownGetsLazyContinuationRightWhereALineScannerCannot(t *testing.T) {
	text := "- first line of the item\ncontinuation with no marker of its own"
	spans := ParseMarkdown(text)

	sp, ok := findRole(spans, RoleList)
	if !ok {
		t.Fatalf("no list span in %v", spans)
	}
	if got, want := spanText(text, sp), "-"; got != want {
		t.Errorf("list span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownSpansInlineCodeInsideANestedListItem is the nested-depth
// case: a list item one level inside another list still gets RoleList on
// its own marker and RoleCode on the inline code inside it, which the old
// hand-rolled scanner could only get right by accident (it read every
// line's own leading bytes the same way regardless of nesting) rather than
// because it understood the structure.
func TestParseMarkdownSpansInlineCodeInsideANestedListItem(t *testing.T) {
	text := "- outer item\n  - inner item with `code`"
	spans := ParseMarkdown(text)

	var listSpans []Span
	for _, sp := range spans {
		if sp.Role == RoleList {
			listSpans = append(listSpans, sp)
		}
	}
	if len(listSpans) != 2 {
		t.Fatalf("got %d list spans, want 2 (outer and inner): %v", len(listSpans), spans)
	}

	codeSp, ok := findRole(spans, RoleCode)
	if !ok {
		t.Fatalf("no code span in %v", spans)
	}
	if got, want := spanText(text, codeSp), "`code`"; got != want {
		t.Errorf("code span covers %q, want %q", got, want)
	}
}

// TestParseMarkdownEmphasisSurvivesBeingInsideALink covers a shape the
// hand-rolled parser's single linear scan over a line could not nest:
// emphasis inside a link's own text. The AST has an Emphasis node as a
// child of the Link node, so both spans exist independently rather than
// one swallowing the other.
func TestParseMarkdownEmphasisSurvivesBeingInsideALink(t *testing.T) {
	text := "[*important*](https://example.com/x)"
	spans := ParseMarkdown(text)

	linkSp, ok := findRole(spans, RoleLink)
	if !ok {
		t.Fatalf("no link span in %v", spans)
	}
	if got, want := spanText(text, linkSp), "https://example.com/x"; got != want {
		t.Errorf("link span covers %q, want %q", got, want)
	}

	emSp, ok := findRole(spans, RoleEmphasis)
	if !ok {
		t.Fatalf("no emphasis span in %v", spans)
	}
	if got, want := spanText(text, emSp), "*important*"; got != want {
		t.Errorf("emphasis span covers %q, want %q", got, want)
	}
}
