package oracle

import (
	"regexp"
	"strings"
	"testing"
)

// The differential compares error *categories*, not text: two engines are not
// obliged to word an error identically, and pinning every message would drown
// real divergences in noise (see README.md).
//
// The template lane still wants the text, because BAML surfaces the engine's
// message to a caller when a prompt fails to render. So message parity is
// checked here separately, as a soft surface with an explicit exception list:
// a row whose wording drifts fails, and a row that starts agreeing has to be
// removed from the list deliberately.
//
// Comparison is on the message body: the kind prefix ("syntax error: ") and the
// location suffix ("(in corpus.txt:1)" / "(at corpus.txt line 1)") are
// formatting, and the two implementations format them differently by design.

var (
	locationSuffix = regexp.MustCompile(` \((in|at) [^)]*\)$`)
	kindPrefix     = regexp.MustCompile(`^[a-z ]+: `)
)

// messageDivergences are the rows where the two engines word an error
// differently, each with the reason and the slice that owns it. Everything else
// must match exactly.
var messageDivergences = map[string]string{
	"err/unknown-filter":               `filter wording ("filter X is unknown" vs the bare name) — environment surface, slice 5`,
	"tmpl/filter-block-unknown-filter": `same filter wording, reached through the block form — slice 5`,
	"tmpl/loop-unknown-method":         `the receiver's kind name: Rust calls the loop object a map, this fork calls it a callable — value-model naming, slice 4`,
}

func messageBody(s string) string {
	s = locationSuffix.ReplaceAllString(s, "")
	return kindPrefix.ReplaceAllString(s, "")
}

func TestErrorMessagesMatchTheEngine(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadCorpora(root + "/" + CorpusDir)
	if err != nil {
		t.Fatal(err)
	}

	compared := 0
	diverged := map[string]bool{}
	for _, corpus := range corpora {
		harness, _, err := LoadHarnessOutcomes(root, corpus)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range corpus.Rows {
			rust, ok := harness.Lookup(row.ID)
			if !ok {
				continue
			}
			got := RunFork(row)
			if rust.Status != StatusError || got.Status != StatusError {
				continue
			}
			compared++
			want, have := messageBody(rust.Message), messageBody(got.Message)
			reason, expected := messageDivergences[row.ID]
			switch {
			case want == have && expected:
				t.Errorf("%s: the messages now agree (%q); remove it from messageDivergences", row.ID, have)
			case want == have:
			case expected:
				diverged[row.ID] = true
				t.Logf("%s: known wording divergence (%s)\n  rust: %q\n  go:   %q", row.ID, reason, want, have)
			default:
				t.Errorf("%s: error text differs from the engine\n  rust: %q\n  go:   %q\n"+
					"  Fix the message, or add the row to messageDivergences with the reason.",
					row.ID, want, have)
			}
		}
	}

	for id := range messageDivergences {
		if !diverged[id] {
			t.Errorf("messageDivergences lists %q, but no corpus row produced an error pair for it", id)
		}
	}

	// A comparison that silently stopped covering anything would pass, so the
	// count is asserted to stay in the range the corpus actually provides.
	if compared < 50 {
		t.Errorf("only %d error rows compared; the error corpus should be larger than that", compared)
	}
	t.Logf("%d error messages compared, %d known wording divergences", compared, len(diverged))
}

// TestSyntaxErrorsReportALocation pins the other half of "exact errors": a
// compile failure has to carry a position, not just text.
func TestSyntaxErrorsReportALocation(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadCorpora(root + "/" + CorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, corpus := range corpora {
		for _, row := range corpus.Rows {
			got := RunFork(row)
			if got.Status != StatusError || got.Category != "syntax" {
				continue
			}
			if !strings.Contains(got.Message, "corpus.txt line ") {
				t.Errorf("%s: syntax error carries no location: %q", row.ID, got.Message)
			}
		}
	}
}
