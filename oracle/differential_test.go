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

	t.Logf("rust engine: %s@%s (%s), %s/%s, outcomes %s",
		report.Provenance.EngineRepo, report.Provenance.EngineRev[:7],
		report.Provenance.EngineBranch, report.Provenance.OS, report.Provenance.Arch,
		report.Source)

	counts := report.Counts()
	t.Logf("%d rows: %d match, %d known divergence",
		len(report.Corpus.Rows), counts[VerdictMatch], counts[VerdictKnownDivergence])

	for _, res := range report.Results {
		res := res
		t.Run(res.Row.ID, func(t *testing.T) {
			switch res.Verdict {
			case VerdictMatch:
				// agreed
			case VerdictKnownDivergence:
				t.Logf("known %s divergence: %s\n  rust: %s\n  go:   %s",
					res.Class, res.Note, res.Rust.Describe(), res.Go.Describe())
			case VerdictNewDivergence:
				t.Errorf("UNDECLARED DIVERGENCE (provisional class %s)\n"+
					"  source: %s\n  rust: %s\n  go:   %s\n"+
					"  If real, add it to oracle/divergences.json with\n"+
					"    rust_signature: %q\n    go_signature:   %q",
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
	corpus, err := LoadCorpus(root + "/" + DefaultCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Rows) == 0 {
		t.Fatal("corpus is empty")
	}
	surfaces := map[string]int{}
	for _, row := range corpus.Rows {
		surfaces[row.Surface]++
	}
	// The seed corpus is deliberately small but must span the surface, so a
	// regression cannot hide by being in an untested category.
	for _, want := range []string{"arithmetic", "comparison", "string", "container", "value_cmp"} {
		if surfaces[want] == 0 {
			t.Errorf("corpus has no rows for surface %q", want)
		}
	}
}
