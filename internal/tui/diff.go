package tui

import (
	"strings"
)

// Diff roles.
//
// Three, rather than one role with two shades, for the same reason the tool
// identities are three: a reader who cannot tell an added line from a removed one
// cannot read a diff at all, and that is a property to test rather than assume.
//
// The hunk header is a fourth, held apart from the two because it is structure
// rather than content: it is the only line in the block that is neither added nor
// removed, and colouring it like either would put a false claim on it.
const (
	// RoleDiffAdd is a line the patch adds.
	RoleDiffAdd Role = 15
	// RoleDiffDel is a line the patch removes.
	RoleDiffDel Role = 16
	// RoleDiffHunk is an `@@` header naming a range.
	RoleDiffHunk Role = 17
)

// RoleNone is the absence of a role, for a row a diff block did not classify.
//
// It is a value rather than an omission so that the roles of a block can run
// parallel to its rows. A span carrying it is dropped when the row is drawn, since
// there is nothing to paint.
const RoleNone Role = -1

// diffFences are the fence languages that are diffs.
//
// The set is small on purpose. A fence tagged `diff` is a promise by the model
// that the block is a patch, and acting on that promise for any language whose
// name happens to end in "diff" would colour prose. The exact set is the whole of
// it: `diff` and `patch` are the two names a model reaches for, and a name not on
// the list is left as plain code rather than guessed at.
var diffFences = map[string]bool{
	"diff":  true,
	"patch": true,
}

// IsDiffFence reports whether a fence info string promises a patch.
//
// The comparison ignores case and surrounding space, since a model writes a space
// after the marker as readily as none, and a reader should not be shown a plain
// block because of it. Anything after the first word is ignored, which is how
// `diff -u` is still a diff.
func IsDiffFence(info string) bool {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return false
	}
	return diffFences[strings.ToLower(fields[0])]
}

// DiffSpans returns the role each row of a patch carries, in row order.
//
// A row is classified by the byte its first non-space character is, which is what
// a patch format guarantees: `+` added, `-` removed, `@` a hunk header when the
// row opens with `@@`. Nothing is inferred from the content, so a line of prose
// inside a patch that happens to begin with a dash is a removal, which is what it
// is.
//
// A row that is neither carries RoleNone rather than being dropped, so the roles
// run parallel to the rows.
func DiffSpans(rows []string) []Role {
	out := make([]Role, len(rows))
	for i, row := range rows {
		out[i], _ = diffRowRole(row)
	}
	return out
}

// diffRowRole classifies one row of a patch, and reports how far the row's own
// text begins past any indentation.
//
// The offset is reported because a span covering a whole row would colour the
// indentation too, and the indentation of a patch row is not part of the change
// being shown. A diff the model indented inside a list is the case: tinting the
// leading spaces as an addition would put four columns of colour on a row that
// added no columns at all.
func diffRowRole(row string) (Role, int) {
	body := strings.TrimLeft(row, " \t")
	indent := len(row) - len(body)

	switch {
	case strings.HasPrefix(body, "@@"):
		return RoleDiffHunk, indent
	case strings.HasPrefix(body, "+++"), strings.HasPrefix(body, "---"):
		// The file names in a unified diff header take the same marks as the lines
		// they introduce, and colour them as content would be a lie about a line
		// that adds nothing and removes nothing.
		return RoleDiffHunk, indent
	case strings.HasPrefix(body, "+"):
		return RoleDiffAdd, indent
	case strings.HasPrefix(body, "-"):
		return RoleDiffDel, indent
	default:
		return RoleNone, 0
	}
}

// DiffRows splits a reply into rows and the spans a diff block contributes.
//
// This is the one place a diff is recognised, so the draw path, the copy path and
// the search path cannot disagree about which rows are a patch. A fence not
// tagged as a diff is passed through as ordinary rows with no spans from this
// function, which is what keeps an ordinary code block looking ordinary.
//
// The fence marker is recognised anywhere on a line rather than only at the start,
// since a model indents a fence when it writes it inside a list. A block whose
// opening marker is not found is a block the renderer would then fold as though it
// were code, which breaks it.
func DiffRows(reply string) ([]string, []Span) {
	var (
		texts []string
		spans []Span
	)

	inBlock, isDiff := false, false
	var block []string

	flush := func() {
		if inBlock && isDiff {
			// The offset is accumulated row by row rather than found by searching
			// the reply for the row's text, since a patch row that appears twice
			// would otherwise be given the offset of the earlier copy.
			offset := len(strings.Join(texts, "\n"))
			if len(texts) > 0 {
				offset++
			}

			for _, row := range block {
				role, indent := diffRowRole(row)
				if role != RoleNone {
					start := offset + indent
					spans = append(spans, Span{
						Start: start,
						End:   start + len(row) - indent,
						Role:  role,
					})
				}
				offset += len(row) + 1
			}
		}

		texts = append(texts, block...)
		inBlock, isDiff, block = false, false, nil
	}

	for _, line := range strings.Split(reply, "\n") {
		_, info, fence := fenceOf(line)

		switch {
		case fence && !inBlock:
			inBlock, isDiff = true, IsDiffFence(info)
			block = []string{line}
		case fence && inBlock:
			block = append(block, line)
			flush()
		case inBlock:
			block = append(block, line)
		default:
			texts = append(texts, line)
		}
	}
	flush()

	return texts, spans
}

// fenceOf reports whether a line opens or closes a fence, and with what.
//
// The opener carries an info string and the closer does not, and either form is
// three backticks or three tildes, which is what the CommonMark rule allows and
// what a model writes. The marker itself is not returned, since a block opened
// with backticks and closed with tildes is a block the model wrote badly and the
// reader is better served by it closing than by the client hunting for its own
// opener.
func fenceOf(line string) (marker, info string, fence bool) {
	trimmed := strings.TrimLeft(line, " \t")

	for _, m := range []string{"```", "~~~"} {
		if !strings.HasPrefix(trimmed, m) {
			continue
		}
		rest := strings.TrimLeft(strings.TrimPrefix(trimmed, m), " \t")
		return m, rest, true
	}
	return "", "", false
}
