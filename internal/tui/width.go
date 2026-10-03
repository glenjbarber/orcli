package tui

import (
	"unicode/utf8"
)

// displayWidth is how many terminal columns a string occupies.
//
// It is columns and not bytes and not runes, and all three differ. A box-drawing rule
// is three bytes and one column, so a byte count reports it three times too wide and
// wraps a layout that is exactly as wide as it was meant to be. A rune count is right
// for most text and wrong for the East Asian wide ranges, which take two columns each,
// so a line of Japanese measures half again as long as its rune count says and the fold
// cuts it in the wrong place.
//
// The rules are the ones a terminal applies, which is the point: the fold and the cut
// have to agree with what the reader sees, and a width this package disagrees with is a
// width that wraps or truncates text the reader can read whole.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// runeWidth is how many columns one rune takes.
//
// Zero for a mark, since it is drawn on top of the character before it rather than
// beside it, and two for a wide one. A zero-width rune counted as one pushes every
// column after it right, which shows as a fold in the wrong place and a line a row wider
// than the terminal.
//
// The zero-width test is the mark ranges rather than the `unicode.Mn` table alone, since
// `Mc` and `Me` are marks too and a spacing mark still attaches to what precedes it on the
// terminals this client runs on. `unicode` does not carry an East Asian width table, so
// the wide set is spelled below too.
func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case isMark(r):
		return 0
	case isWide(r):
		return 2
	default:
		return 1
	}
}

// isMark reports whether a rune is drawn on top of what precedes it.
//
// It is spelled rather than imported because the answer is a property of how a terminal
// draws rather than of the character, and `unicode` has no predicate for the drawing
// property.
func isMark(r rune) bool { return runeIn(r, markRanges) }

// isWide reports whether a rune is drawn two columns wide.
func isWide(r rune) bool { return runeIn(r, wideRanges) }

// runeIn reports whether a rune falls in a table of ranges.
//
// One function rather than one per property, since the tables have the same shape and a
// second property would otherwise copy the loop. The scan stops at the first range whose
// start is above the rune, which is only correct while the tables are ascending, and a
// test holds them to that.
func runeIn(r rune, table [][2]rune) bool {
	for _, span := range table {
		if r < span[0] {
			// The tables are ascending, so a rune below this range's start is below every
			// later one as well.
			return false
		}
		if r <= span[1] {
			return true
		}
	}
	return false
}

// markRanges is the mark set: combining marks, spacing marks and enclosing marks.
//
// It is a list rather than a fixed-size array so that both tables can be handed to one
// scan function, and so a reader looking at it sees a list rather than an array type with
// a length in it.
var markRanges = [][2]rune{
	{0x0300, 0x036f}, // Combining diacritical marks.
	{0x0483, 0x0489}, // Cyrillic combining.
	{0x0591, 0x05bd}, // Hebrew points.
	{0x05bf, 0x05bf}, // Hebrew point.
	{0x05c1, 0x05c2}, // Hebrew points.
	{0x05c4, 0x05c5}, // Hebrew marks.
	{0x05c7, 0x05c7}, // Hebrew point.
	{0x0610, 0x061a}, // Arabic marks.
	{0x064b, 0x065f}, // Arabic marks.
	{0x0670, 0x0670}, // Arabic superscript alef.
	{0x06d6, 0x06dc}, // Arabic marks.
	{0x06df, 0x06e4}, // Arabic marks.
	{0x06e7, 0x06e8}, // Arabic marks.
	{0x06ea, 0x06ed}, // Arabic marks.
	{0x0711, 0x0711}, // Syriac.
	{0x0730, 0x074a}, // Syriac marks.
	{0x07a6, 0x07b0}, // Arabic marks.
	{0x0816, 0x0819}, // Samaritan marks.
	{0x081b, 0x0823}, // Samaritan marks.
	{0x0825, 0x0827}, // Samaritan marks.
	{0x0829, 0x082d}, // Samaritan marks.
	{0x0900, 0x0903}, // Devanagari marks.
	{0x093a, 0x093c}, // Devanagari marks.
	{0x093e, 0x094f}, // Devanagari vowel signs.
	{0x0951, 0x0957}, // Devanagari stress.
	{0x0962, 0x0963}, // Devanagari marks.
	{0x0e31, 0x0e31}, // Thai vowel sign.
	{0x0e34, 0x0e3a}, // Thai vowel signs.
	{0x0e47, 0x0e4e}, // Thai tone marks.
	{0x1ab0, 0x1aff}, // Combining marks extended.
	{0x1dc0, 0x1dff}, // Combining marks supplement.
	{0x20d0, 0x20f0}, // Combining marks for symbols.
	{0xfe00, 0xfe0f}, // Variation selectors.
	{0xfe20, 0xfe2f}, // Combining half marks.
}

// wideRanges is the East Asian wide and fullwidth set, as start and end pairs inclusive.
//
// These are the ranges a terminal gives two columns: the CJK blocks and their extensions,
// the Hangul syllables and jamo, the fullwidth forms, and the emoji a terminal draws at
// double width.
//
// The set is spelled rather than imported because the standard library has no width
// table and a dependency for a fold is a dependency every build carries. Halfwidth and
// narrow forms are absent on purpose: a terminal draws those one column, which is what
// treating them as ordinary text already does.
//
// A test checks the table is ascending and does not overlap, since the scan in runeIn
// stops at the first range whose start is above the rune.
var wideRanges = [][2]rune{
	{0x1100, 0x115f},   // Hangul Jamo, initial consonants.
	{0x231a, 0x231b},   // Watch, hourglass.
	{0x2329, 0x232a},   // Angle brackets.
	{0x23e9, 0x23ec},   // Media controls.
	{0x23f0, 0x23f0},   // Alarm clock.
	{0x23f3, 0x23f3},   // Hourglass, flowing.
	{0x25fd, 0x25fe},   // Small squares.
	{0x2614, 0x2615},   // Umbrella, hot beverage.
	{0x2648, 0x2653},   // Zodiac.
	{0x267f, 0x267f},   // Wheelchair.
	{0x2693, 0x2693},   // Anchor.
	{0x26a1, 0x26a1},   // High voltage.
	{0x26aa, 0x26ab},   // Circles.
	{0x26bd, 0x26be},   // Soccer, baseball.
	{0x26c4, 0x26c5},   // Snowman, sun behind cloud.
	{0x26ce, 0x26ce},   // Ophiuchus.
	{0x26d4, 0x26d4},   // No entry.
	{0x26ea, 0x26ea},   // Mosque.
	{0x26f2, 0x26f3},   // Fountain, golf.
	{0x26f5, 0x26f5},   // Sailboat.
	{0x26fa, 0x26fa},   // Tent.
	{0x26fd, 0x26fd},   // Fuel pump.
	{0x2705, 0x2705},   // Check mark button.
	{0x270a, 0x270b},   // Raised fist and hand.
	{0x2728, 0x2728},   // Sparkles.
	{0x274c, 0x274c},   // Cross mark.
	{0x274e, 0x274e},   // Cross mark button.
	{0x2753, 0x2755},   // Question and exclamation ornaments.
	{0x2757, 0x2757},   // Red exclamation.
	{0x2795, 0x2797},   // Plus, minus, divide.
	{0x27b0, 0x27b0},   // Curly loop.
	{0x27bf, 0x27bf},   // Double curly loop.
	{0x2b1b, 0x2b1c},   // Large squares.
	{0x2b50, 0x2b50},   // Star.
	{0x2b55, 0x2b55},   // Hollow red circle.
	{0x2e80, 0x2e99},   // CJK radicals.
	{0x2e9b, 0x2ef3},   // CJK radicals.
	{0x2f00, 0x2fd5},   // Kangxi radicals.
	{0x2ff0, 0x2ffb},   // Ideographic description.
	{0x3000, 0x303e},   // CJK symbols and punctuation.
	{0x3041, 0x3096},   // Hiragana.
	{0x3099, 0x30ff},   // Combining marks, Katakana.
	{0x3105, 0x312f},   // Bopomofo.
	{0x3131, 0x318e},   // Hangul compatibility Jamo.
	{0x3190, 0x31e3},   // Kanbun, Bopomofo, strokes.
	{0x31ef, 0x321e},   // Katakana extensions, enclosed CJK.
	{0x3220, 0x3247},   // Enclosed CJK.
	{0x3250, 0x4dbf},   // Enclosed CJK, CJK extension A.
	{0x4e00, 0xa48c},   // CJK unified ideographs, Yi syllables.
	{0xa490, 0xa4c6},   // Yi radicals.
	{0xa960, 0xa97c},   // Hangul Jamo extended A.
	{0xac00, 0xd7a3},   // Hangul syllables.
	{0xf900, 0xfaff},   // CJK compatibility ideographs.
	{0xfe10, 0xfe19},   // Vertical forms.
	{0xfe30, 0xfe52},   // CJK compatibility forms, small forms.
	{0xfe54, 0xfe6b},   // Small form variants.
	{0xff01, 0xff60},   // Fullwidth forms.
	{0xffe0, 0xffe6},   // Fullwidth signs.
	{0x16fe0, 0x16fe4}, // Tangut and Nushu marks.
	{0x17000, 0x18cd5}, // Tangut, Khitan small script.
	{0x1b000, 0x1b152}, // Kana supplement and extended.
	{0x1b164, 0x1b167}, // Small Kana extension.
	{0x1b170, 0x1b2fb}, // Nushu.
	{0x1f004, 0x1f004}, // Mahjong red dragon.
	{0x1f0cf, 0x1f0cf}, // Playing card.
	{0x1f18e, 0x1f18e}, // Negative squared AB.
	{0x1f191, 0x1f19a}, // Squared symbols.
	{0x1f200, 0x1f320}, // Enclosed ideographic supplement and emoji.
	{0x1f32d, 0x1f37c}, // Emoji.
	{0x1f37e, 0x1f393}, // Emoji.
	{0x1f3a0, 0x1f3ca}, // Emoji.
	{0x1f3cf, 0x1f3d3}, // Emoji.
	{0x1f3e0, 0x1f3f0}, // Emoji.
	{0x1f3f4, 0x1f3f4}, // Waving black flag.
	{0x1f3f8, 0x1f43e}, // Emoji.
	{0x1f440, 0x1f440}, // Eyes.
	{0x1f442, 0x1f4fc}, // Emoji.
	{0x1f4ff, 0x1f53d}, // Emoji.
	{0x1f54b, 0x1f54e}, // Emoji.
	{0x1f550, 0x1f567}, // Clock faces.
	{0x1f57a, 0x1f57a}, // Man dancing.
	{0x1f595, 0x1f596}, // Hand gestures.
	{0x1f5a4, 0x1f5a4}, // Black heart.
	{0x1f5fb, 0x1f64f}, // Emoji.
	{0x1f680, 0x1f6c5}, // Transport and map.
	{0x1f6cc, 0x1f6cc}, // Person in bed.
	{0x1f6d0, 0x1f6d2}, // Emoji.
	{0x1f6d5, 0x1f6d7}, // Emoji.
	{0x1f6eb, 0x1f6ec}, // Airplane departure and arrival.
	{0x1f6f4, 0x1f6fc}, // Emoji.
	{0x1f7e0, 0x1f7eb}, // Geometric shapes extended.
	{0x1f90c, 0x1f93a}, // Emoji.
	{0x1f93c, 0x1f945}, // Emoji.
	{0x1f947, 0x1f9ff}, // Emoji.
	{0x1fa70, 0x1fa7c}, // Geometric shapes extended.
	{0x1fa80, 0x1fa89}, // Geometric shapes extended.
	{0x1fac6, 0x1fadc}, // Emoji.
	{0x1fadf, 0x1fae8}, // Emoji.
	{0x1faf0, 0x1faf8}, // Emoji.
	{0x20000, 0x2fffd}, // CJK extensions B through F, and compatibility.
	{0x30000, 0x3fffd}, // CJK extension G.
}

// DisplayWidth reports how many terminal columns a string occupies.
//
// It is exported because the draw path, the fold, the paste preview and the search all
// have to agree about it. Four places that each measure for themselves is four places
// that can disagree, and a fold that disagrees with the terminal produces a line the
// reader has to scroll sideways to read.
func DisplayWidth(s string) int { return displayWidth(s) }

// CutColumn cuts a string to a column budget from the front.
//
// The cut is at a rune boundary and not at a byte, since a rune cut in half is a
// replacement character on the reader's screen and bytes the copy path would carry into
// whatever the reader pasted them into.
//
// A string already inside the budget is returned whole with nothing dropped, so a caller
// does not have to ask which case it is in.
func CutColumn(s string, columns int) (string, int) {
	if columns <= 0 {
		return "", displayWidth(s)
	}
	if displayWidth(s) <= columns {
		return s, 0
	}

	w, cut := 0, len(s)
	for i, r := range s {
		rw := runeWidth(r)
		if w+rw > columns {
			cut = i
			break
		}
		w += rw
	}
	return s[:cut], displayWidth(s[cut:])
}

// ellipsis marks a row that was cut rather than folded.
//
// It is a named constant rather than a literal at each use, since three places cutting a
// row and one place measuring it is a marker spelled two ways and a width reserved
// differently from the width it takes.
const ellipsis = "…"

// CutColumnFromEnd cuts a string to a column budget keeping the end rather than the
// start.
//
// The tail is what a reader needs from a row too wide for the terminal. The case is a
// tool line whose end is the command it ran: the beginning of a long path is the same
// directory as the beginning of every other row, and the filename is what tells them
// apart.
//
// The marker is counted against the budget rather than added to it, since a row that
// overruns by the width of its own marker is a row that wraps, which is the failure this
// exists to prevent.
//
// A row is filled as far as the budget allows rather than cut at the first character
// that does not fit. A wide character one column too wide for the space left would
// otherwise leave a gap, and a row that is five columns in a six column budget reads as
// broken rather than tight.
func CutColumnFromEnd(s string, columns int) string {
	if columns <= 0 {
		return ""
	}
	if displayWidth(s) <= columns {
		return s
	}

	markerWidth := displayWidth(ellipsis)
	if columns <= markerWidth {
		// No room for the marker and for content. The marker alone tells the reader the
		// row was cut, which is the part that matters; the content it replaced was
		// unreadable at this width anyway.
		return ellipsis
	}

	// Walked backwards so the cut lands at a rune boundary, then filled forwards from
	// the cut to reclaim any space a wide character left behind.
	keep := columns - markerWidth

	w := 0
	cut := len(s)
	for cut > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:cut])
		rw := runeWidth(r)
		if w+rw > keep {
			break
		}
		w += rw
		cut -= size
	}

	// The character before the cut may be narrow enough to fit where the one after it
	// was not, so the tail is extended one rune at a time while there is room.
	for cut < len(s) {
		r, size := utf8.DecodeRuneInString(s[cut:])
		rw := runeWidth(r)
		if w+rw > keep {
			break
		}
		w += rw
		cut += size
	}

	return ellipsis + s[cut:]
}

// fit reports the column budget a row has, given the columns taken by its gutter.
//
// A gutter is not a decoration here: the level and the copy handle lead every row, so a
// row folded to the full terminal width would push its own markers off the right edge
// and the level a reader is copying by would no longer be on the row it belongs to.
func fit(total, gutter int) int {
	if gutter >= total {
		// A terminal narrower than its own gutter leaves nothing for the text. One column
		// is still worth drawing, since a row with no text is a blank line and a blank
		// line in a log reads as a gap rather than as content.
		return 1
	}
	return total - gutter
}
