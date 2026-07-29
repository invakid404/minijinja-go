// Package unicodecase reproduces the Unicode string operations the engine's
// builtins are built from.
//
// The engine's `|upper` is Rust's `str::to_uppercase`, its `|trim` is
// `str::trim`, and pycompat's `.isalpha()` is `char::is_alphabetic`. Go's
// standard library answers several of those differently, and the differences
// are not exotic:
//
//   - Go's strings.ToUpper is *simple* case mapping, one rune in, one rune
//     out. Rust's is *full* case mapping: "ß" uppercases to "SS", "ﬁ" to "FI",
//     "İ" lowercases to two scalars.
//   - Rust's str::to_lowercase implements the Unicode final-sigma rule, so
//     "ΑΣ" lowercases to "ας" and "ΑΣΑ" to "ασα". Go always produces σ.
//   - Rust's char::is_lowercase / is_uppercase / is_alphabetic are the Unicode
//     Lowercase / Uppercase / Alphabetic *properties*. Go's unicode.IsLower /
//     IsUpper / IsLetter are the Ll / Lu / L categories, which are narrower —
//     they disagree on ~11k scalars for Alphabetic alone.
//
// The tables in tables.go are dumped from the reference implementation itself
// (oracle/harness/src/casegen.rs) rather than reconstructed from the Unicode
// data files by hand, and the package test replays that dump in full. So the
// agreement is by construction, and a Rust-toolchain Unicode version bump
// shows up as a failing test rather than as a mystery divergence in a prompt.
package unicodecase

import (
	"strings"
	"unicode/utf8"
)

type runeRange struct{ lo, hi rune }

func inRanges(ranges []runeRange, r rune) bool {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := int(uint(lo+hi) >> 1)
		switch {
		case r < ranges[mid].lo:
			hi = mid - 1
		case r > ranges[mid].hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// IsLower reports char::is_lowercase: the Unicode Lowercase property.
func IsLower(r rune) bool { return inRanges(lowercaseRanges, r) }

// IsUpper reports char::is_uppercase: the Unicode Uppercase property.
func IsUpper(r rune) bool { return inRanges(uppercaseRanges, r) }

// IsAlphabetic reports char::is_alphabetic: the Unicode Alphabetic property.
func IsAlphabetic(r rune) bool { return inRanges(alphabeticRanges, r) }

// IsNumeric reports char::is_numeric: the Unicode N general category.
func IsNumeric(r rune) bool { return inRanges(numericRanges, r) }

// IsAlphanumeric reports char::is_alphanumeric.
func IsAlphanumeric(r rune) bool { return IsAlphabetic(r) || IsNumeric(r) }

// IsSpace reports char::is_whitespace: the Unicode White_Space property.
func IsSpace(r rune) bool { return inRanges(whitespaceRanges, r) }

// isCased reports the Unicode Cased property.
func isCased(r rune) bool { return inRanges(casedRanges, r) }

// isCaseIgnorable reports the Unicode Case_Ignorable property.
func isCaseIgnorable(r rune) bool { return inRanges(caseIgnorableRanges, r) }

// ToUpperRune is char::to_uppercase, the per-character full mapping. It is
// what `title` and `capitalize` apply to a single character.
func ToUpperRune(r rune) string {
	if mapped, ok := upperMap[r]; ok {
		return mapped
	}
	return string(r)
}

// ToLowerRune is char::to_lowercase. Unlike ToLower it has no context, so a
// capital sigma always becomes σ — which is exactly what `title` does.
func ToLowerRune(r rune) string {
	if mapped, ok := lowerMap[r]; ok {
		return mapped
	}
	return string(r)
}

// ToUpper is str::to_uppercase.
func ToUpper(s string) string {
	var b strings.Builder
	for i, r := range s {
		if mapped, ok := upperMap[r]; ok {
			if b.Len() == 0 {
				b.Grow(len(s) + 8)
				b.WriteString(s[:i])
			}
			b.WriteString(mapped)
			continue
		}
		if b.Len() != 0 {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

// ToLower is str::to_lowercase, including the Greek final-sigma rule: a
// capital sigma lowercases to ς at the end of a word and to σ everywhere else.
func ToLower(s string) string {
	var b strings.Builder
	for i, r := range s {
		var mapped string
		switch {
		case r == 'Σ':
			mapped = "σ"
			if isWordFinal(s, i) {
				mapped = "ς"
			}
		default:
			var ok bool
			if mapped, ok = lowerMap[r]; !ok {
				if b.Len() != 0 {
					b.WriteRune(r)
				}
				continue
			}
		}
		if b.Len() == 0 {
			b.Grow(len(s) + 8)
			b.WriteString(s[:i])
		}
		b.WriteString(mapped)
	}
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

// isWordFinal reports whether the sigma at byte offset i is at the end of a
// word: preceded by a cased character (ignoring case-ignorable ones) and not
// followed by one. This is str::to_lowercase's case_ignorable_then_cased, run
// backwards over the prefix and forwards over the suffix.
func isWordFinal(s string, i int) bool {
	before := false
	for j := i; j > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
		if isCaseIgnorable(r) {
			continue
		}
		before = isCased(r)
		break
	}
	if !before {
		return false
	}
	for _, r := range s[i+len("Σ"):] {
		if isCaseIgnorable(r) {
			continue
		}
		return !isCased(r)
	}
	return true
}

// TrimSpace is str::trim.
func TrimSpace(s string) string { return strings.TrimFunc(s, IsSpace) }

// TrimLeftSpace is str::trim_start.
func TrimLeftSpace(s string) string { return strings.TrimLeftFunc(s, IsSpace) }

// TrimRightSpace is str::trim_end.
func TrimRightSpace(s string) string { return strings.TrimRightFunc(s, IsSpace) }

// Fields is str::split_whitespace: the non-empty runs between Unicode
// whitespace.
func Fields(s string) []string {
	return strings.FieldsFunc(s, IsSpace)
}

// FoldCompare is `UniCase::cmp`: the engine's case-insensitive string ordering.
//
// `sort` and `groupby` do not lowercase and compare. Under the `unicode`
// feature — which BAML's build enables — `cmp_helper` wraps both sides in
// `unicase::UniCase` and compares those (filters.rs:284-300), and UniCase
// orders by the Unicode case-FOLDED character sequence. The two are different
// functions and the difference is visible on ordinary input:
//
//   - folding is length changing. "ß" folds to "ss", so `['ß','ss','SS']|sort`
//     is one tie, not two strings around a third.
//   - folding is not lowercasing. "İ" folds to "i" + U+0307, which compares
//     EQUAL to "i̇" — where lowercasing puts "İ" first.
//   - comparison is by character, not by UTF-8 bytes, so a fold that produces a
//     shorter sequence sorts before a longer one with the same prefix.
//
// UniCase picks an ASCII fast path when both strings are ASCII, but that path
// agrees character for character with the Unicode one over ASCII — the fold of
// 'A'..'Z' is exactly `to_ascii_lowercase` — so one code path reproduces both.
//
// The fold table is unicase's own, dumped per scalar (see tables.go). It is
// tabulated as the ITERATION order of unicase's `Fold`, which for a
// three-character fold is not the folded order: `Fold::Three(a, b, c)` yields
// c, then a, then b, so "ﬃ" iterates as "iff". That is a quirk of the pinned
// crate and it is load-bearing — it decides `['ﬃ','ffi']|sort` — so it is
// reproduced rather than corrected.
func FoldCompare(a, b string) int {
	ra, rb := a, b
	var bufA, bufB []rune
	for {
		var ca, cb rune
		var okA, okB bool
		ca, ra, bufA, okA = nextFolded(ra, bufA)
		cb, rb, bufB, okB = nextFolded(rb, bufB)
		switch {
		case !okA && !okB:
			return 0
		case !okA:
			return -1
		case !okB:
			return 1
		case ca < cb:
			return -1
		case ca > cb:
			return 1
		}
	}
}

// FoldEqual reports whether two strings are case-insensitively equal under the
// engine's comparator. It is FoldCompare == 0 and is spelled separately because
// that is the question `groupby` asks between adjacent items.
func FoldEqual(a, b string) bool { return FoldCompare(a, b) == 0 }

// nextFolded yields the next character of a string's folded sequence, without
// materializing the whole fold. buf carries the tail of a multi-character fold.
func nextFolded(s string, buf []rune) (rune, string, []rune, bool) {
	if len(buf) > 0 {
		return buf[0], s, buf[1:], true
	}
	for s != "" {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		folded, ok := unicaseFoldMap[r]
		if !ok {
			return r, s, nil, true
		}
		runes := []rune(folded)
		if len(runes) == 0 {
			// unicase has no empty fold, but a table that grew one must not
			// silently drop the rest of the string.
			continue
		}
		return runes[0], s, runes[1:], true
	}
	return 0, "", nil, false
}

// RustcVersion reports the toolchain the tables were dumped from.
func RustcVersion() string { return rustcVersion }
