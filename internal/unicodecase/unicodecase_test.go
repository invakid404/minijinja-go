package unicodecase

import (
	"encoding/json"
	"os"
	"testing"
)

// dump is the reference implementation's answer for every Unicode scalar,
// produced by oracle/harness/src/casegen.rs.
type dump struct {
	SchemaVersion int         `json:"schema_version"`
	Rustc         string      `json:"rustc"`
	ToUpper       []mapping   `json:"to_uppercase"`
	ToLower       []mapping   `json:"to_lowercase"`
	Lowercase     [][2]rune   `json:"lowercase"`
	Uppercase     [][2]rune   `json:"uppercase"`
	Alphabetic    [][2]rune   `json:"alphabetic"`
	Numeric       [][2]rune   `json:"numeric"`
	Whitespace    [][2]rune   `json:"whitespace"`
	Cased         [][2]rune   `json:"cased"`
	CaseIgnorable [][2]rune   `json:"case_ignorable"`
	Samples       [][6]string `json:"samples"`
}

type mapping struct {
	Rune rune
	To   string
}

func (m *mapping) UnmarshalJSON(data []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if err := json.Unmarshal(pair[0], &m.Rune); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &m.To)
}

func load(t *testing.T) *dump {
	t.Helper()
	raw, err := os.ReadFile("testdata/rust-unicode.json")
	if err != nil {
		t.Fatalf("reading the reference dump: %v", err)
	}
	var d dump
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("parsing the reference dump: %v", err)
	}
	return &d
}

// TestTablesMatchReference replays the whole dump. It is what makes the claim
// "this package is Unicode-identical to the engine" checkable rather than
// asserted: every scalar the reference maps, and every scalar it does not, is
// compared.
func TestTablesMatchReference(t *testing.T) {
	d := load(t)
	if d.Rustc != rustcVersion {
		t.Fatalf("the dump was produced by %q but the tables were generated from %q; regenerate both",
			d.Rustc, rustcVersion)
	}

	for _, m := range d.ToUpper {
		if got := ToUpperRune(m.Rune); got != m.To {
			t.Errorf("ToUpperRune(%q) = %q, want %q", m.Rune, got, m.To)
		}
	}
	for _, m := range d.ToLower {
		if got := ToLowerRune(m.Rune); got != m.To {
			t.Errorf("ToLowerRune(%q) = %q, want %q", m.Rune, got, m.To)
		}
	}

	// Every scalar the reference does NOT remap must be left alone, and every
	// property must hold over exactly the reference's ranges.
	upper := setOf(d.ToUpper)
	lower := setOf(d.ToLower)
	props := []struct {
		name string
		in   func(rune) bool
		want map[rune]bool
	}{
		{"IsLower", IsLower, rangeSet(d.Lowercase)},
		{"IsUpper", IsUpper, rangeSet(d.Uppercase)},
		{"IsAlphabetic", IsAlphabetic, rangeSet(d.Alphabetic)},
		{"IsNumeric", IsNumeric, rangeSet(d.Numeric)},
		{"IsSpace", IsSpace, rangeSet(d.Whitespace)},
		{"isCased", isCased, rangeSet(d.Cased)},
		{"isCaseIgnorable", isCaseIgnorable, rangeSet(d.CaseIgnorable)},
	}

	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue // surrogates are not scalars
		}
		if _, remapped := upper[r]; !remapped {
			if got := ToUpperRune(r); got != string(r) {
				t.Errorf("ToUpperRune(%q) = %q, want the character unchanged", r, got)
			}
		}
		if _, remapped := lower[r]; !remapped {
			if got := ToLowerRune(r); got != string(r) {
				t.Errorf("ToLowerRune(%q) = %q, want the character unchanged", r, got)
			}
		}
		for _, p := range props {
			if got, want := p.in(r), p.want[r]; got != want {
				t.Errorf("%s(%q) = %v, want %v", p.name, r, got, want)
			}
		}
	}
}

// TestSamplesMatchReference covers the string-level operations, where the
// per-scalar tables are not the whole story: the final-sigma rule is context
// sensitive and trimming is a property of the whole string.
func TestSamplesMatchReference(t *testing.T) {
	for _, sample := range load(t).Samples {
		in, wantUpper, wantLower := sample[0], sample[1], sample[2]
		wantTrim, wantTrimStart, wantTrimEnd := sample[3], sample[4], sample[5]
		if got := ToUpper(in); got != wantUpper {
			t.Errorf("ToUpper(%q) = %q, want %q", in, got, wantUpper)
		}
		if got := ToLower(in); got != wantLower {
			t.Errorf("ToLower(%q) = %q, want %q", in, got, wantLower)
		}
		if got := TrimSpace(in); got != wantTrim {
			t.Errorf("TrimSpace(%q) = %q, want %q", in, got, wantTrim)
		}
		if got := TrimLeftSpace(in); got != wantTrimStart {
			t.Errorf("TrimLeftSpace(%q) = %q, want %q", in, got, wantTrimStart)
		}
		if got := TrimRightSpace(in); got != wantTrimEnd {
			t.Errorf("TrimRightSpace(%q) = %q, want %q", in, got, wantTrimEnd)
		}
	}
}

func setOf(ms []mapping) map[rune]string {
	rv := make(map[rune]string, len(ms))
	for _, m := range ms {
		rv[m.Rune] = m.To
	}
	return rv
}

func rangeSet(ranges [][2]rune) map[rune]bool {
	rv := make(map[rune]bool)
	for _, r := range ranges {
		for c := r[0]; c <= r[1]; c++ {
			rv[c] = true
		}
	}
	return rv
}
