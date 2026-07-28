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
