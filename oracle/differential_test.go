package oracle

import (
	"testing"
)

// TestDifferential runs every corpus row through this fork and against the
// pinned Rust engine's outcome.
//
// It fails on an undeclared divergence, on a declared divergence that has
// changed shape or disappeared, and on a ledger entry with no corpus row. A
// declared, unchanged divergence passes and is reported: those are the permanent
// regression rows the later semantic slices have to close.
func TestDifferential(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, run := range report.Runs {
		t.Logf("corpus %s: %d rows against %s@%s (%s), %s/%s, outcomes %s",
			run.Corpus.Name, len(run.Corpus.Rows),
			run.Provenance.EngineRepo, run.Provenance.EngineRev[:7],
			run.Provenance.EngineBranch, run.Provenance.OS, run.Provenance.Arch,
			run.Source)
	}

	counts := report.Counts()
	t.Logf("%d rows: %d match, %d known divergence",
		report.Rows(), counts[VerdictMatch], counts[VerdictKnownDivergence])

	for _, res := range report.Results {
		res := res
		t.Run(res.Row.ID, func(t *testing.T) {
			switch res.Verdict {
			case VerdictMatch:
				// agreed
			case VerdictKnownDivergence:
				// A declared divergence is a record for the slice that closes
				// it — except in a lane whose slice has landed, whose exit gate
				// is byte-exactness with no decline. Declaring one there must
				// not be a way to turn this test green.
				if res.IsNumericClass() {
					t.Errorf("NUMERIC ROW IS A KNOWN DIVERGENCE, which slice 3's gate does not accept\n"+
						"  source: %s\n  rust: %s\n  go:   %s\n"+
						"  ledger note: %s\n"+
						"  Close the mismatch; a numeric row may not be declared, refused or declined.",
						res.Row.TemplateSource(), res.Rust.Describe(), res.Go.Describe(), res.Note)
					return
				}
				t.Logf("known %s divergence: %s\n  rust: %s\n  go:   %s",
					res.Class, res.Note, res.Rust.Describe(), res.Go.Describe())
			case VerdictNewDivergence:
				t.Errorf("UNDECLARED DIVERGENCE (provisional class %s)\n"+
					"  source: %s\n  rust: %s\n  go:   %s\n"+
					"  If real, add it to oracle/divergences.json with\n"+
					"    rust_signatures: [%q]\n    go_signatures:   [%q]",
					res.Class, res.Row.TemplateSource(),
					res.Rust.Describe(), res.Go.Describe(),
					res.Rust.Signature(), res.Go.Signature())
			case VerdictLedgerStale:
				t.Errorf("STALE LEDGER ENTRY: %s\n  rust: %s\n  go:   %s",
					res.Note, res.Rust.Describe(), res.Go.Describe())
			case VerdictMissing:
				t.Errorf("%s", res.Note)
			default:
				t.Errorf("unknown verdict %q", res.Verdict)
			}
		})
	}
}

// TestCorpusIsMeaningful guards the differential against quietly becoming a
// no-op: an empty corpus, or one where nothing is actually being compared,
// would otherwise pass.
func TestCorpusIsMeaningful(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadCorpora(root + "/" + CorpusDir)
	if err != nil {
		t.Fatal(err)
	}

	// Each lane must span the surfaces it exists to cover, so a regression
	// cannot hide by being in an untested category.
	want := map[string][]string{
		"seed":     {"arithmetic", "comparison", "string", "container", "value_cmp"},
		"template": {"control", "whitespace", "macros", "loader"},
		"numeric":  {"arithmetic", "comparison", "conversion", "rendering"},
	}
	seen := map[string]bool{}
	for _, corpus := range corpora {
		seen[corpus.Name] = true
		if len(corpus.Rows) == 0 {
			t.Errorf("corpus %s is empty", corpus.Name)
		}
		surfaces := map[string]int{}
		for _, row := range corpus.Rows {
			surfaces[row.Surface]++
		}
		for _, surface := range want[corpus.Name] {
			if surfaces[surface] == 0 {
				t.Errorf("corpus %s has no rows for surface %q", corpus.Name, surface)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("corpus %s is missing entirely", name)
		}
	}
}

// TestNumericClassIsByteExact is slice 3's merge gate, stated once and
// independently of how any individual row is classified.
//
// TestDifferential already fails on a numeric known-divergence, but it does so
// row by row, which means the gate is only as good as the classification. This
// asserts the property directly and from the other side: the ledger may not
// contain an entry for a numeric row at all. A numeric mismatch therefore has
// nowhere to go — it cannot be undeclared (TestDifferential fails), and it
// cannot be declared (this fails).
//
// SCOPE. "Byte-exact" here means the compared outcome signature: status,
// rendered bytes, normalized boolean and error category. One field is
// deliberately outside it — the DIAGNOSTIC of a panic, which is the host
// language runtime's message rather than either engine's. That field is pinned
// from both sides by panics_test.go rather than compared; see the contract
// stated there. Nothing else is out of scope, and nothing is skipped.
func TestNumericClassIsByteExact(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadCorpora(root + "/" + CorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadLedger(root + "/" + DefaultLedgerPath)
	if err != nil {
		t.Fatal(err)
	}

	numeric := make(map[string]bool)
	for _, corpus := range corpora {
		for _, row := range corpus.Rows {
			if corpus.Name == NumericLane || row.Surface == "arithmetic" {
				numeric[row.ID] = true
			}
		}
	}
	if len(numeric) == 0 {
		t.Fatal("no numeric rows in the corpus; the gate would be vacuous")
	}
	for _, entry := range ledger.Entries {
		if numeric[entry.ID] {
			t.Errorf("ledger declares a divergence for numeric row %q: %s\n"+
				"  Slice 3's exit gate is byte-exact with no decline, so a numeric row "+
				"may not be carried as a declared divergence. Close it, or STOP with a "+
				"design proposal rather than declaring it.",
				entry.ID, entry.Summary)
		}
	}

	report, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	exact := 0
	for _, res := range report.Results {
		if !res.IsNumericClass() {
			continue
		}
		if res.Verdict != VerdictMatch {
			t.Errorf("numeric row %q is %s, not an exact match\n  rust: %s\n  go:   %s",
				res.Row.ID, res.Verdict, res.Rust.Describe(), res.Go.Describe())
			continue
		}
		exact++
	}
	t.Logf("%d numeric rows, all byte-exact on the compared signature "+
		"(panic diagnostics are pinned separately; see panics_test.go)", exact)
	if exact != len(numeric) {
		t.Errorf("%d of %d numeric rows are byte-exact", exact, len(numeric))
	}
}

// TestNumericCorpusCoversTheModel pins the numeric corpus's coverage by row id.
//
// The differential proves that whatever is in the corpus agrees; it cannot
// notice a case that was quietly dropped. These are the rows the numeric model
// was built against — the boundaries the float64 path could not represent, the
// operators whose Rust semantics differ from Go's, the integral-float promotion
// class the #649 forensics reverse-engineered, and one row per float-to-int
// consumer the engine has. Removing any of them has to be a deliberate edit
// here as well.
func TestNumericCorpusCoversTheModel(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadCorpora(root + "/" + CorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	numeric := 0
	for _, corpus := range corpora {
		for _, row := range corpus.Rows {
			have[row.ID] = true
			if corpus.Name == NumericLane {
				numeric++
			}
		}
	}

	required := map[string][]string{
		"2^53 and the i64 edge": {
			"num/add-2pow53-plus-one", "num/sub-2pow53-adjacent", "num/sub-i64max",
			"num/add-i64max-widens", "num/mul-2pow62-by-two", "num/eq-2pow53-adjacent",
		},
		"i128 and u128 edges": {
			"num/i128max-literal", "num/i128-min", "num/u128-max-literal",
			"num/u128-pair-wrapping", "num/u128-mixed-refuses",
		},
		"overflow is an error": {
			"num/add-i128max-overflow", "num/mul-i128max-overflow",
			"num/sub-i128-min-overflow", "num/pow-overflow", "num/neg-i128-min-overflow",
		},
		"div, floordiv, rem, pow": {
			"num/floordiv-neg-denominator", "num/floordiv-both-negative",
			"num/floordiv-float-neg-denominator", "num/floordiv-by-zero",
			"num/rem-neg-denominator", "num/rem-both-negative", "num/rem-by-zero",
			"num/div-int-whole", "num/div-zero-by-zero",
			"num/pow-negative-exponent", "num/pow-exponent-past-u32", "num/pow-left-assoc",
		},
		"the #649 integral-float promotion class": {
			"num/pow-float-base-2pow63", "num/pow-promoted-composition",
			"num/float-literal-rounds-to-2pow49", "num/rem-integral-float",
			"num/is-even-2pow63-float",
		},
		"every float-to-int consumer": {
			"num/int-filter-1e30", "num/abs-i64-min", "num/index-float-key",
			"num/slice-frac-start", "num/slice-frac-stop", "num/slice-frac-step",
			"num/range-frac", "num/mul-string-repeat-float", "num/is-even-bigint",
		},
		"float rendering": {
			"num/render-one-third", "num/render-1e300", "num/render-nan",
			"num/render-neg-zero", "num/render-float-epsilon",
		},
		"the total order over floats": {
			"num/lt-neg-zero", "num/lt-nan", "num/gt-nan", "num/lt-neg-nan",
		},
		"the integer-literal U64 repr, both directions": {
			"num/eq-i64max-vs-2pow63-float", "num/eq-u64max-vs-2pow64-float",
			"num/eq-i64max-self",
		},
		"where the engine's Ord panics, and where it does not": {
			"num/lt-int-float-lossy", "num/gt-float-int-lossy",
			"num/lt-i64max-vs-2pow63-float", "num/lt-u128-vs-int",
			"num/sort-uncomparable", "num/eq-u128-vs-int", "num/eq-int-float-lossy",
		},
	}
	for group, ids := range required {
		for _, id := range ids {
			if !have[id] {
				t.Errorf("%s: corpus row %q is missing", group, id)
			}
		}
	}
	if numeric < 140 {
		t.Errorf("numeric corpus has %d rows; it had 146 when the slice landed, so this is a shrink", numeric)
	}
}

// TestTemplateCorpusExercisesEveryProfile keeps the whitespace lane honest: a
// profile nobody uses proves nothing, and a row silently falling back to the
// stock profile would compare the wrong environment on both sides.
func TestTemplateCorpusExercisesEveryProfile(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadCorpora(root + "/" + CorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	used := map[Profile]int{}
	for _, corpus := range corpora {
		for _, row := range corpus.Rows {
			used[row.Profile]++
		}
	}
	for profile := range KnownProfiles {
		if used[profile] == 0 {
			t.Errorf("no corpus row uses profile %q", profile)
		}
	}
}
