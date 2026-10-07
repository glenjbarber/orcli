package tui

import "strings"

// ParseMarkdown reads a reply for the markdown shapes the five orphaned roles
// were defined for - heading, list, quote, code and link - and returns the
// spans that colour them.
//
// It is called from Deliver rather than from cmd/orcli, which is the seam
// this package already draws everywhere else: internal/tui owns rendering,
// cmd/orcli owns the model conversation, and ask.go hands Deliver the whole
// reply text exactly as the model wrote it, never a styled one. A parser
// living in cmd/orcli would mean every caller of Deliver - and there is more
// than one, Begin's own reply among them - would have to remember to call it
// first, and a caller that forgot would silently ship plain text. A parser
// inside Deliver means every reply is styled the same way with nothing for a
// caller to remember.
//
// It is a function of the whole text, not a per-line one, because a fenced
// code block's state - inside a fence or not - has to carry from one line to
// the next, and a per-line parser would have nowhere to keep it.
//
// # What is in scope and what is not
//
// ATX headings (one to six leading '#'), unordered and ordered list markers,
// blockquote lines, fenced and inline code, and `[text](url)` links are
// built. Bold (`**`) and italic (`*`/`_`) both map to RoleEmphasis rather
// than being left out: RoleEmphasis's own doc comment already says "bold or
// italic", and every existing caller of it - the reader's echoed question,
// a screen-break notice - uses it for exactly that register, text set apart
// from the sentence around it without naming a further distinction the
// palette has no role for. A parser that invented a sixth role for italic
// alone would be adding a role this task was not asked to add.
//
// A link's own doc comment, RoleLink, says "a link target" - target, not
// text - so only the URL inside the parens is given the role, not the
// bracketed text in front of it. The alternative, colouring both, would
// make an ordinary sentence's worth of link text read entirely in the link
// colour, which is what a reader scanning for "what here is a link" does not
// want: the colour should land on the one part of the line that is actually
// the destination.
func ParseMarkdown(text string) []Span {
	var spans []Span
	inFence := false

	lineStart := 0
	for lineStart <= len(text) {
		nl := strings.IndexByte(text[lineStart:], '\n')
		var line string
		var lineEnd int
		if nl < 0 {
			line = text[lineStart:]
			lineEnd = len(text)
		} else {
			line = text[lineStart : lineStart+nl]
			lineEnd = lineStart + nl
		}

		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)

		if strings.HasPrefix(trimmed, "```") {
			// The fence marker itself is code, open or close, and the state
			// flips either way: a close fence is still part of the block it
			// closes, and colouring it like the body is what makes clear where
			// the block ends rather than leaving the marker looking like plain
			// text next to coloured code above it.
			spans = append(spans, Span{Start: lineStart + indent, End: lineEnd, Role: RoleCode})
			inFence = !inFence
		} else if inFence {
			if strings.TrimSpace(line) != "" {
				spans = append(spans, Span{Start: lineStart, End: lineEnd, Role: RoleCode})
			}
		} else {
			spans = append(spans, parseLinePrefix(line, lineStart)...)
			spans = append(spans, parseInline(line, lineStart)...)
		}

		if nl < 0 {
			break
		}
		lineStart = lineStart + nl + 1
	}

	return spans
}

// headingPrefixLen reports how many bytes of line, from its first non-space
// byte, an ATX heading marker occupies - the '#' run and the one space after
// it - or zero if line is not a heading.
//
// Between one and six, with a run of seven or more not a heading at all: that
// is the ATX rule this follows rather than a simplification of it, and a
// parser that coloured a comment made entirely of '#' characters as a
// heading would be guessing at a shape the model did not intend.
func headingPrefixLen(trimmed string) int {
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n >= len(trimmed) || trimmed[n] != ' ' {
		return 0
	}
	return n + 1
}

// listMarkerLen reports how many bytes of trimmed an unordered or ordered
// list marker occupies, including the one space after it, or zero if trimmed
// does not open with one.
func listMarkerLen(trimmed string) int {
	if len(trimmed) >= 2 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && trimmed[1] == ' ' {
		return 2
	}

	// An ordered marker is a run of digits, a dot, then a space - "1. " is
	// the shortest shape, so three bytes is the floor rather than two.
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(trimmed) || trimmed[i] != '.' || trimmed[i+1] != ' ' {
		return 0
	}
	return i + 2
}

// parseLinePrefix spans a line's own leading marker - heading, list or
// quote - at most one of the three, since a line is only ever one of those
// shapes. offset is where line begins in the whole reply, which every span
// this returns is rebased against.
func parseLinePrefix(line string, offset int) []Span {
	trimmed := strings.TrimLeft(line, " ")
	indent := len(line) - len(trimmed)

	if n := headingPrefixLen(trimmed); n > 0 {
		// Past the '#' run and the one space after it: the heading's own
		// text is what reads as the heading, and the marker itself is
		// scaffolding a reader has no reason to see highlighted.
		return []Span{{Start: offset + indent + n, End: offset + len(line), Role: RoleHeading}}
	}

	if n := listMarkerLen(trimmed); n > 0 {
		// Only the marker itself - the bullet or the "1." - takes RoleList.
		// The item's own text is read as whatever it contains (plain, or
		// further inline spans from parseInline), the same as a line beside
		// it with no marker at all; the marker is what says "this is a list
		// item" and the text is whatever it is on its own terms.
		return []Span{{Start: offset + indent, End: offset + indent + n - 1, Role: RoleList}}
	}

	if strings.HasPrefix(trimmed, ">") {
		// The whole quoted line, past the indent, reads as the quote: unlike
		// a list marker a '>' carries no further structure worth separating
		// from the text after it, and a reader following a blockquote across
		// several lines is reading it as one voice set apart, not as a marker
		// plus body on each line.
		return []Span{{Start: offset + indent, End: offset + len(line), Role: RoleQuote}}
	}

	return nil
}

// parseInline spans the markdown that can appear mid-line: inline code,
// bold/italic emphasis, and links. offset is where line begins in the whole
// reply.
func parseInline(line string, offset int) []Span {
	var spans []Span

	i := 0
	for i < len(line) {
		switch {
		case line[i] == '`':
			if end := strings.IndexByte(line[i+1:], '`'); end >= 0 {
				close := i + 1 + end
				spans = append(spans, Span{Start: offset + i, End: offset + close + 1, Role: RoleCode})
				i = close + 1
				continue
			}

		case strings.HasPrefix(line[i:], "**"):
			if end := strings.Index(line[i+2:], "**"); end >= 0 {
				close := i + 2 + end
				spans = append(spans, Span{Start: offset + i, End: offset + close + 2, Role: RoleEmphasis})
				i = close + 2
				continue
			}

		case line[i] == '*' || line[i] == '_':
			// Single '*' or '_' is italic, checked after the double-asterisk
			// case above so "**bold**" is not read as two adjacent italic
			// spans with an empty one between them. A marker immediately
			// followed by a space is punctuation rather than emphasis - "* "
			// opens nothing - the same shape listMarkerLen already treats as
			// a list bullet rather than italic at the start of a line.
			if i+1 < len(line) && line[i+1] != ' ' {
				if end := strings.IndexByte(line[i+1:], line[i]); end >= 0 {
					close := i + 1 + end
					spans = append(spans, Span{Start: offset + i, End: offset + close + 1, Role: RoleEmphasis})
					i = close + 1
					continue
				}
			}

		case line[i] == '[':
			if textEnd := strings.IndexByte(line[i+1:], ']'); textEnd >= 0 {
				afterText := i + 1 + textEnd + 1
				if afterText < len(line) && line[afterText] == '(' {
					if urlEnd := strings.IndexByte(line[afterText+1:], ')'); urlEnd >= 0 {
						urlStart := afterText + 1
						urlClose := urlStart + urlEnd
						// Only the URL, between the parens, takes RoleLink -
						// see ParseMarkdown's own doc comment for why the
						// bracketed text in front of it does not.
						spans = append(spans, Span{Start: offset + urlStart, End: offset + urlClose, Role: RoleLink})
						i = urlClose + 1
						continue
					}
				}
			}
		}
		i++
	}

	return spans
}
