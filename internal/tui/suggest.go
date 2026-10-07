package tui

import "sort"

// levenshtein returns the edit distance between a and b: the fewest single-character
// insertions, deletions and substitutions that turn one into the other.
//
// It is the standard dynamic-programming table rather than a cleverer algorithm,
// because the names this is run against are a handful of characters long and run once
// per mistyped command, not once per keystroke the way Complete's prefix matching
// does. There is no case here where the simple version is too slow to be worth the
// trouble a faster one would be to read.
//
// It is written from scratch rather than pulled in as a dependency, on the same
// grounds the rest of this module's third-party list is short: go.mod has no
// fuzzy-matching library already, and Levenshtein distance is small and well
// understood enough that adding one to get it would be adding a dependency for code
// a reader could have had explained to them in the time it took to fetch it.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	// prev and curr are the two rows of the table that are ever read: the one just
	// finished and the one being built. The full table is never kept, since nothing
	// after this function reads anything but the final distance, and a row-wide pair
	// is the whole of what filling the last cell needs.
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

// maxSuggestThreshold caps how large suggestThreshold ever grows, no matter how long
// the typed name is.
//
// Without a cap, a long name's half-length threshold keeps climbing - five for
// freemodels, five for attribution - and at that distance nearly any other word in
// the table of a similar length starts to qualify, which is the same over-matching
// problem a flat threshold causes for short names, just reached from the other
// direction. Two is picked rather than three because this table's longer names
// (permission, attribution, freemodels, cloudflare) do not sit close enough to each
// other, or to anything shorter, to need more room than that: the real typos this
// hint exists for are a wrong, missing, doubled or transposed letter or two, not a
// word half-rewritten.
const maxSuggestThreshold = 2

// suggestThreshold is the largest edit distance a candidate name may be from a typed
// name and still be offered as a "did you mean" guess.
//
// It is half the typed name's length, rounded down, capped at maxSuggestThreshold -
// not a flat distance-2 cutoff, because this table's real names skew short (key, new
// and btw are three characters; pane, save, load, main, copy, info, bell, test, exit
// and quit are four), and a flat 2 would let a three-letter name match a sizeable
// slice of the alphabet: almost anything three or four letters long is within 2 edits
// of key. Scaling the threshold with the typed word's own length keeps a short typo
// meaning a short, plausible slip rather than a different word entirely: key or new,
// at three characters, gets threshold 1 (half of 3, rounded down), so a single wrong
// letter is still caught but two is treated as a different word on purpose.
//
// Four characters is where the half-length threshold reaches the cap of 2 and stays
// there: /qiot for /quit is exactly this case - quit is four characters, and qiot is
// two substitutions away from it (distance 2) - and a reader who typed that specific
// slip is the example this feature is for, so the threshold has to clear it rather
// than only clearing single-letter typos once a name reaches four letters. Above
// four characters the half-length value would keep climbing past the cap on its own
// (five for a ten-letter name like freemodels or attribution), so the cap holds every
// longer name to the same distance-2 ceiling a four-letter name already sits at,
// instead of getting looser the longer the word is.
func suggestThreshold(name string) int {
	t := len(name) / 2
	if t < 1 {
		t = 1
	}
	if t > maxSuggestThreshold {
		t = maxSuggestThreshold
	}
	return t
}

// Suggest returns the command names closest to a typed, unrecognised name, for a "did
// you mean" hint.
//
// It is built over every name byName holds, hidden ones included, rather than only
// the names Names() lists. Hidden names exist for a reader who already knows the old
// spelling, such as /colour or /cognito; a reader who mistypes one of those still
// mistyped a real command this build answers to, and pointing them at nothing because
// the real target happens to be unlisted would withhold the one hint that is actually
// useful to them. The entry's listed name is what gets suggested either way - a
// hidden name is a spelling worth accepting, not one worth recommending - so a typo of
// /cognito is offered /stealth, the name that spelling is hidden behind.
//
// Only names within that length's threshold (see suggestThreshold) are considered at
// all, and only the ones at the smallest distance found are returned: a name 1 edit
// away is a better guess than one 2 edits away, and offering both as equally likely
// would make the closer match no more prominent than the farther one. The result is
// capped at three names. A tie beyond three is not expected in this table - no typo
// has yet landed equidistant from four or more real names - but the cap exists so a
// pathological case prints a short, readable hint rather than every command in the
// table at once. Ties are broken alphabetically, so the same typo always produces the
// same hint rather than one that depends on map iteration order.
func Suggest(name string) []string {
	threshold := suggestThreshold(name)

	best := threshold + 1
	var atBest []string

	// byName has one key per spelling a command answers to - its listed name, each
	// alias, each hidden name - so the loop already sees every spelling without
	// reaching into a command's own Aliases or Hidden fields. What is tracked
	// per command is the closest spelling found so far, since a reader who typed
	// something close to /cognito should not be skipped over just because
	// /stealth, the name that gets suggested, happens to be farther away.
	bestForCommand := make(map[string]int, len(byName))
	for typed, c := range byName {
		if typed == name {
			continue
		}

		d := levenshtein(name, typed)
		if cur, ok := bestForCommand[c.Name]; !ok || d < cur {
			bestForCommand[c.Name] = d
		}
	}

	for cmdName, d := range bestForCommand {
		if d > threshold {
			continue
		}

		switch {
		case d < best:
			best = d
			atBest = []string{cmdName}
		case d == best:
			atBest = append(atBest, cmdName)
		}
	}

	sort.Strings(atBest)
	if len(atBest) > 3 {
		atBest = atBest[:3]
	}
	return atBest
}
