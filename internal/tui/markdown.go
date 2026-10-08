package tui

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gmtext "github.com/yuin/goldmark/text"
)

// ParseMarkdown reads a reply for the markdown shapes the five orphaned
// roles were defined for - heading, list, quote, code and link - and
// returns the spans that colour them.
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
// # Why goldmark rather than the hand-rolled scanner this replaced
//
// The previous version of this file scanned the reply line by line, with a
// single boolean (inFence) carried from one line to the next for state. That
// was enough for an ATX heading or a fenced block that opened and closed
// cleanly, and wrong for everything CommonMark actually allows a block to do:
// a list item's own paragraph continuing on the next line with no marker
// (lazy continuation), a blockquote nested inside a list item, emphasis
// whose delimiter run has to be measured against the runes on both sides
// to tell "a*b*c" from "*emphasis*", and a dozen smaller rules besides.
// CommonMark's grammar is not line-regular - whether a given line is part of
// a paragraph, a list item, or a blockquote depends on the lines around it,
// not on itself - so a parser that looks at one line at a time cannot get
// every case right no matter how much is added to it. Glen's own instruction
// after testing the old parser live ("screwey") was to build this from an
// upstream module rather than keep extending the hand-rolled one, which is
// what this file now does: goldmark parses the whole reply once into a real
// AST, and this file walks that tree rather than the text.
//
// What stays the same: the function signature (ParseMarkdown(text string)
// []Span), called the same way from Deliver, and the five roles it maps
// onto. What changes is that every span below is read off a node the parser
// has already decided is a heading, a list item, a blockquote, a code run or
// a link - not guessed from a line's own leading bytes - so a line that is
// only a list item because the paragraph three lines above opened one is
// coloured correctly, which the line scanner had no way to know.
//
// # What is in scope and what is not
//
// ATX and setext headings, list item markers (ordered and unordered, at any
// nesting depth), blockquote lines, fenced and indented code blocks, inline
// code, and inline links are built. Bold and italic both still map to
// RoleEmphasis rather than a role of their own: RoleEmphasis's own doc
// comment already says "bold or italic", and every existing caller of it -
// the reader's echoed question, a screen-break notice - uses it for exactly
// that register, text set apart from the sentence around it without naming
// a further distinction the palette has no role for. Having a real AST does
// not change that judgment: goldmark's Emphasis node carries a Level (1 for
// `*x*`, 2 for `**x**`) but still only one node kind, the same single
// distinction the old parser drew by hand.
//
// A link's own doc comment, RoleLink, says "a link target" - target, not
// text - and that judgment is kept rather than revised now that the AST
// cleanly separates a Link node's text children from its Destination: only
// the URL takes RoleLink, not the bracketed text in front of it. The
// reasoning is the same as before and is now easier to state with the real
// tree in hand rather than guessed at: colouring the link text too would
// make an ordinary sentence's worth of words read entirely in the link
// colour, which is not what a reader scanning for "what here is a link"
// wants - they want the colour on the one part of the line that is actually
// the destination, and the AST makes that part unambiguous instead of a
// matter of counting parens by hand.
func ParseMarkdown(text string) []Span {
	source := []byte(text)
	doc := goldmark.DefaultParser().Parse(gmtext.NewReader(source))

	var spans []Span
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindHeading:
			if start, end, ok := nodeRange(n); ok {
				spans = append(spans, Span{Start: start, End: end, Role: RoleHeading})
			}
		case ast.KindListItem:
			if sp, ok := listMarkerSpan(n, source); ok {
				spans = append(spans, sp)
			}
		case ast.KindBlockquote:
			spans = append(spans, blockquoteSpans(n, source)...)
		case ast.KindFencedCodeBlock:
			spans = append(spans, fencedCodeBlockSpans(n.(*ast.FencedCodeBlock), source)...)
		case ast.KindCodeBlock:
			spans = append(spans, codeContentSpans(n.Lines(), source)...)
		case ast.KindCodeSpan:
			if start, end, ok := codeSpanRange(n, source); ok {
				spans = append(spans, Span{Start: start, End: end, Role: RoleCode})
			}
		case ast.KindEmphasis:
			if start, end, ok := emphasisRange(n.(*ast.Emphasis), source); ok {
				spans = append(spans, Span{Start: start, End: end, Role: RoleEmphasis})
			}
		case ast.KindLink:
			if start, end, ok := linkDestinationRange(n.(*ast.Link), source); ok {
				spans = append(spans, Span{Start: start, End: end, Role: RoleLink})
			}
		}
		return ast.WalkContinue, nil
	})

	return spans
}

// linesOf reports a node's own Lines, for the block node kinds that carry
// them (Paragraph, Heading, CodeBlock, FencedCodeBlock, TextBlock), or ok
// false for a node kind that does not, such as an inline node.
func linesOf(n ast.Node) (*gmtext.Segments, bool) {
	// BaseInline implements the same Lines() method as BaseBlock, but panics
	// if it is actually called on an inline node ("can not call with inline
	// nodes") - so the interface check alone is not enough, and the node's
	// own Type() has to be asked first.
	if n.Type() != ast.TypeBlock {
		return nil, false
	}
	lb, ok := n.(interface{ Lines() *gmtext.Segments })
	if !ok {
		return nil, false
	}
	lines := lb.Lines()
	if lines == nil {
		return nil, false
	}
	return lines, true
}

// nodeRange reports the byte range in source a node's own content covers:
// the start of its first line or first text child, and the end of its last.
// It is how a heading's span is found - a heading's text is its children,
// not a Lines() range of its own in every build of the tree - and it is the
// building block codeSpanRange, emphasisRange and linkDestinationRange grow
// outward from to recover the delimiters goldmark drops once it has used
// them.
func nodeRange(n ast.Node) (start, end int, ok bool) {
	s := nodeStart(n)
	e := nodeEnd(n)
	if s < 0 || e < 0 || e < s {
		return 0, 0, false
	}
	return s, e, true
}

func nodeStart(n ast.Node) int {
	if lines, ok := linesOf(n); ok && lines.Len() > 0 {
		return lines.At(0).Start
	}
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Start
	}
	if p := n.Pos(); p >= 0 {
		return p
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if s := nodeStart(c); s >= 0 {
			return s
		}
	}
	return -1
}

func nodeEnd(n ast.Node) int {
	if lines, ok := linesOf(n); ok && lines.Len() > 0 {
		return lines.At(lines.Len() - 1).Stop
	}
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Stop
	}
	for c := n.LastChild(); c != nil; c = c.PreviousSibling() {
		if e := nodeEnd(c); e >= 0 {
			return e
		}
	}
	return -1
}

// codeSpanRange recovers an inline code span's full range, backticks
// included, from the range its content alone leaves once goldmark has
// trimmed the one leading and trailing space CommonMark allows inside the
// delimiters. The content's own start and end sit just inside the
// backticks (and, if that trim happened, just inside one more space), so
// this walks outward over at most one space and then the backtick run on
// each side to recover what the content range left out.
func codeSpanRange(n ast.Node, source []byte) (start, end int, ok bool) {
	start = nodeStart(n)
	end = nodeEnd(n)
	if start < 0 || end < 0 {
		return 0, 0, false
	}
	// The space is only part of the span if it is itself the one CommonMark
	// trims, which means a backtick sits right before it - a space here that
	// is just the ordinary text before the code run (as in "run `code`")
	// must not be pulled in.
	if start > 1 && source[start-1] == ' ' && source[start-2] == '`' {
		start--
	}
	for start > 0 && source[start-1] == '`' {
		start--
	}
	if end+1 < len(source) && source[end] == ' ' && source[end+1] == '`' {
		end++
	}
	for end < len(source) && source[end] == '`' {
		end++
	}
	return start, end, true
}

// emphasisRange recovers an Emphasis node's full range, delimiters included.
// Unlike inline code, CommonMark emphasis delimiters touch the text with no
// space allowed between them, so the node's own Level (1 for `*x*`/`_x_`, 2
// for `**x**`/`__x__`) is exactly how many bytes of marker sit on each side,
// whichever of '*' or '_' the reply actually used.
func emphasisRange(n *ast.Emphasis, source []byte) (start, end int, ok bool) {
	// Emphasis's own Pos() is already the opening delimiter's start -
	// goldmark's delimiter parser sets it there directly, unlike most inline
	// nodes - so unlike codeSpanRange's content-outward walk, the marker on
	// this side is simply read off the node. There is no equivalent for the
	// closing delimiter, so that side still grows outward from the content.
	start = n.Pos()
	if start < 0 {
		if s := nodeStart(n); s >= 0 {
			start = s - n.Level
		}
	}
	end = nodeEnd(n)
	if end >= 0 {
		end += n.Level
	}
	if start < 0 || end < 0 || end > len(source) {
		return 0, 0, false
	}
	return start, end, true
}

// linkDestinationRange finds an inline link's destination in the source
// text, not the Destination field goldmark already parsed onto the node -
// that field is the resolved value, unescaped, and this needs the raw byte
// range instead so a Span can point into the reply exactly as it was
// written. The link's text ends where its last child ends; the ']' closing
// it, the '(' opening the destination, and the first ')' after that are
// found from there the same way the hand-rolled parser's IndexByte calls
// did, except the starting point now comes from the AST rather than from
// scanning for an unescaped '[' by hand, so a link nested inside a list
// item or a blockquote is found correctly instead of by coincidence.
func linkDestinationRange(n *ast.Link, source []byte) (start, end int, ok bool) {
	textEnd := nodeEnd(n)
	if textEnd < 0 {
		textEnd = n.Pos()
	}
	if textEnd < 0 {
		return 0, 0, false
	}

	i := textEnd
	for i < len(source) && source[i] != ']' {
		i++
	}
	if i >= len(source) {
		return 0, 0, false
	}
	i++ // past ']'.
	if i >= len(source) || source[i] != '(' {
		return 0, 0, false
	}
	i++ // past '('.

	j := i
	for j < len(source) && source[j] != ')' {
		j++
	}
	if j >= len(source) {
		return 0, 0, false
	}
	return i, j, true
}

// listMarkerSpan finds a list item's own marker - the bullet or the
// "N." - and nothing else: the item's text is read as whatever it contains
// on its own terms, exactly as the hand-rolled parser already decided, the
// same way a line beside it with no marker at all is read. What changes
// from the hand-rolled version is where the marker is found: a ListItem's
// own Pos() is set, generically, to the byte where the block parser opened
// it - the marker's own first byte, indentation already consumed by
// whatever container it is nested in - rather than re-deriving it from a
// line's own leading bytes, which cannot tell a bullet that opens a list
// from one three levels deep in a nested list, or a lazy continuation line
// from a line with its own marker. The marker's end is found by walking
// forward to the first byte of the item's actual content (via the lines its
// descendants carry, never through a node's own Pos(), which for some kinds
// means something other than a source offset) and trimming the run of
// spaces between the marker and that content back off.
func listMarkerSpan(n ast.Node, source []byte) (Span, bool) {
	markerStart := n.Pos()
	if markerStart < 0 {
		return Span{}, false
	}
	contentStart := firstContentStart(n)
	if contentStart < 0 || contentStart > len(source) || contentStart <= markerStart {
		return Span{}, false
	}

	markerEnd := contentStart
	for markerEnd > markerStart && source[markerEnd-1] == ' ' {
		markerEnd--
	}
	if markerEnd <= markerStart {
		return Span{}, false
	}
	return Span{Start: markerStart, End: markerEnd, Role: RoleList}, true
}

// firstContentStart finds the start of a node's own content the same way
// nodeStart does, except it never falls back to a node's own Pos(): for
// some block kinds (ListItem among them) Pos() means something other than
// "where this node's content begins" - see listMarkerSpan - so a caller that
// specifically wants content, not the node's own marker or opening byte,
// uses this instead.
func firstContentStart(n ast.Node) int {
	if lines, ok := linesOf(n); ok && lines.Len() > 0 {
		return lines.At(0).Start
	}
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Start
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if s := firstContentStart(c); s >= 0 {
			return s
		}
	}
	return -1
}

// blockquoteSpans spans every physical source line this blockquote owns,
// past its own leading indent, the same "the whole quoted line reads as the
// quote" rule the hand-rolled parser applied by looking at the line's own
// '>' byte. Lines belonging to a blockquote nested inside this one are left
// for that nested node's own call, so a line is not spanned twice over.
func blockquoteSpans(n ast.Node, source []byte) []Span {
	var starts []int
	collectLineStarts(n, &starts)

	var spans []Span
	seen := map[int]bool{}
	for _, s := range starts {
		lineStart := lineStartOf(source, s)
		if seen[lineStart] {
			continue
		}
		seen[lineStart] = true

		lineEnd := lineEndOf(source, lineStart)
		i := lineStart
		for i < lineEnd && source[i] == ' ' {
			i++
		}
		if i < lineEnd && source[i] == '>' {
			spans = append(spans, Span{Start: i, End: lineEnd, Role: RoleQuote})
		}
	}
	return spans
}

// collectLineStarts gathers the start of every line a node's own content
// covers, not descending into a nested blockquote - that node gets its own
// call from the Walk in ParseMarkdown, and descending here too would spell
// its lines twice.
func collectLineStarts(n ast.Node, out *[]int) {
	if lines, ok := linesOf(n); ok {
		for i := 0; i < lines.Len(); i++ {
			*out = append(*out, lines.At(i).Start)
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Kind() == ast.KindBlockquote {
			continue
		}
		collectLineStarts(c, out)
	}
}

// fencedCodeBlockSpans spans a fenced code block whole: both fence marker
// lines and every content line between them, matching the hand-rolled
// parser's own choice to colour the fence markers themselves rather than
// leave them looking like plain text next to coloured code above and below.
// goldmark's own Lines() for a FencedCodeBlock covers the content only, so
// the fence lines are recovered from the one physical source line
// immediately before the first content line and immediately after the
// last - which is exactly where a fence has to be, since nothing else can
// sit between a fence and the content it opens or closes.
func fencedCodeBlockSpans(n *ast.FencedCodeBlock, source []byte) []Span {
	lines := n.Lines()
	spans := codeContentSpans(lines, source)
	if lines == nil || lines.Len() == 0 {
		return spans
	}

	first := lines.At(0)
	if ls, le, ok := lineBefore(source, first.Start); ok {
		spans = append(spans, Span{Start: ls, End: le, Role: RoleCode})
	}

	// A line's own Stop, per text.Segment's own doc comment on ForceNewline,
	// already sits past that line's trailing '\n' - so the line right after
	// the fenced block's last content line starts exactly at Stop, with no
	// further byte to skip the way there would be if Stop pointed at the
	// newline itself.
	last := lines.At(lines.Len() - 1)
	if ls, le, ok := lineAt(source, last.Stop); ok {
		spans = append(spans, Span{Start: ls, End: le, Role: RoleCode})
	}
	return spans
}

// codeContentSpans spans every non-blank line a code block's own Lines
// covers, blank ones left uncoloured the same way the hand-rolled parser
// skipped a blank line inside a fence: a blank line reads as a gap whether
// or not it is coloured, and a span with nothing on it is one more thing a
// later caller like foldRowLines has to carry for no visible effect.
//
// Each segment's own Stop reaches one past the line's trailing '\n' (see
// fencedCodeBlockSpans's comment on lineAt), which is trimmed back off here
// so the span covers the line's own text and nothing past it - a newline
// has no colour to carry, and leaving it in would count it as code in a
// test that measures a span by its length.
func codeContentSpans(lines *gmtext.Segments, source []byte) []Span {
	if lines == nil {
		return nil
	}
	var spans []Span
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		start, end := seg.Start, seg.Stop
		if end > start && end <= len(source) && source[end-1] == '\n' {
			end--
		}
		if start == end {
			continue
		}
		spans = append(spans, Span{Start: start, End: end, Role: RoleCode})
	}
	return spans
}

// lineStartOf finds where the physical source line containing pos begins.
func lineStartOf(source []byte, pos int) int {
	i := pos
	for i > 0 && source[i-1] != '\n' {
		i--
	}
	return i
}

// lineEndOf finds where the physical source line starting at lineStart
// ends, excluding the newline.
func lineEndOf(source []byte, lineStart int) int {
	i := lineStart
	for i < len(source) && source[i] != '\n' {
		i++
	}
	return i
}

// lineBefore reports the physical line immediately before the one starting
// at contentStart, or ok false if contentStart is the first line in source
// (nothing precedes it to be a fence).
func lineBefore(source []byte, contentStart int) (start, end int, ok bool) {
	if contentStart <= 0 || source[contentStart-1] != '\n' {
		return 0, 0, false
	}
	end = contentStart - 1
	start = lineStartOf(source, end)
	return start, end, true
}

// lineAt reports the physical line starting at pos, or ok false if pos runs
// to the end of source (an unclosed fence has no closing line to find).
func lineAt(source []byte, pos int) (start, end int, ok bool) {
	if pos >= len(source) {
		return 0, 0, false
	}
	start = pos
	end = lineEndOf(source, start)
	return start, end, true
}
