package oracle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// Default locations, relative to the oracle module root.
const (
	CorpusDir         = "corpus"
	RecordedDir       = "recorded"
	DefaultLedgerPath = "divergences.json"
	DefaultHarnessBin = "harness/target/release/mj-oracle-harness"

	// EngineRevShort names the pinned engine revision in recording filenames.
	// It is short on purpose: the full revision, branch and feature set live
	// inside every recording's provenance block.
	EngineRevShort = "8cfc770"
)

// Source says where the Rust-side outcomes came from.
type Source string

const (
	// SourceLive means the Rust harness was executed for this run.
	SourceLive Source = "live"
	// SourceRecorded means a committed harness run was replayed. The recording
	// carries the corpus digest it was produced from, so it cannot be replayed
	// against a corpus it does not correspond to.
	SourceRecorded Source = "recorded"
)

// Verdict is the differential result for one row.
type Verdict string

const (
	// VerdictMatch: both engines produced the same outcome signature.
	VerdictMatch Verdict = "match"
	// VerdictKnownDivergence: they differ, and the ledger declares exactly
	// this difference.
	VerdictKnownDivergence Verdict = "known-divergence"
	// VerdictNewDivergence: they differ and nothing declares it. This is the
	// failure the differential exists to produce.
	VerdictNewDivergence Verdict = "new-divergence"
	// VerdictLedgerStale: the ledger declares a divergence that is no longer
	// there, or that has changed shape.
	VerdictLedgerStale Verdict = "ledger-stale"
	// VerdictMissing: the Rust side has no outcome for this row.
	VerdictMissing Verdict = "missing-harness-result"
)

// NumericLane is the corpus lane slice 3 owns.
const NumericLane = "numeric"

// RowResult is the differential outcome for one corpus row.
type RowResult struct {
	// Corpus is the name of the corpus file the row came from.
	Corpus  string
	Row     Row
	Rust    Outcome
	Go      Outcome
	Verdict Verdict
	Class   Class
	Note    string
}

// IsNumericClass reports whether a row belongs to the numeric core, the class
// slice 3 must make byte-exact.
//
// It is the whole numeric lane plus the arithmetic rows the seed lane already
// carried, so a numeric regression cannot escape the gate by living in the
// older lane.
func (r RowResult) IsNumericClass() bool {
	return r.Corpus == NumericLane || r.Row.Surface == "arithmetic"
}

// CorpusRun records which engine outcomes one corpus file was compared against.
type CorpusRun struct {
	Corpus     *Corpus
	Source     Source
	Provenance Provenance
}

// Report is a full differential run over every corpus file.
type Report struct {
	Runs    []CorpusRun
	Results []RowResult
}

// Rows is the total number of corpus rows the report covered.
func (r *Report) Rows() int {
	n := 0
	for _, run := range r.Runs {
		n += len(run.Corpus.Rows)
	}
	return n
}

// Counts summarizes a report by verdict.
func (r *Report) Counts() map[Verdict]int {
	counts := make(map[Verdict]int)
	for _, res := range r.Results {
		counts[res.Verdict]++
	}
	return counts
}

// Failures returns the results that must fail a differential run.
//
// A DECLARED divergence normally passes: those are the permanent regression
// rows the later semantic slices exist to close, and the ledger is what keeps
// them visible. A lane whose slice has landed is the exception. Slice 3's exit
// gate is byte-exactness with no decline, so a numeric row that is merely
// "known" is a merge blocker rather than a record — otherwise declaring a
// numeric mismatch in the ledger would be enough to turn the gate green, which
// is precisely how a decline would slip through.
//
// A later slice adds its own lane here the same way as it lands.
func (r *Report) Failures() []RowResult {
	var out []RowResult
	for _, res := range r.Results {
		switch res.Verdict {
		case VerdictNewDivergence, VerdictLedgerStale, VerdictMissing:
			out = append(out, res)
		case VerdictKnownDivergence:
			if res.IsNumericClass() {
				out = append(out, res)
			}
		}
	}
	return out
}

// harnessCache memoizes one harness invocation per (binary, corpus) for the
// life of the process.
//
// `go test ./...` sweeps the corpus four times — the differential itself twice,
// then the panic pins and the message comparison — and each sweep used to spawn
// the harness again. That is four evaluations of an input that is deterministic
// by construction, and, because a timed-out row's thread is left spinning until
// its process exits (harness/src/main.rs evaluate_bounded), four concurrent
// CPU burners on whatever machine CI gave us. Both of those are what a longer
// RowTimeout would otherwise multiply: at 30 seconds the un-memoized suite pays
// the deliberate non-terminating row four times over.
//
// Keyed by binary and by the corpus digest, so a different harness, a different
// lane or an edited corpus is never served a stale entry. Recorded replays are
// cached too; reading a file four times is cheap, but the entry has to exist
// under the same key either way.
var harnessCache sync.Map // string -> *harnessCacheEntry

type harnessCacheEntry struct {
	once   sync.Once
	output *HarnessOutput
	source Source
	err    error
}

// LoadHarnessOutcomes obtains the Rust-side outcomes for a corpus.
//
// If a harness binary is available (MJ_ORACLE_HARNESS, or the default release
// build) it is executed live. Otherwise the committed recording is replayed.
// Either way the corpus digest in the provenance must match the corpus that was
// loaded, so a stale recording is an error rather than a silently weaker test.
//
// The result is memoized per process; see [harnessCache].
func LoadHarnessOutcomes(root string, corpus *Corpus) (*HarnessOutput, Source, error) {
	corpusPath := corpus.Path

	// MJ_ORACLE_RECORDED_ONLY forces the replay path even when a harness is
	// present. CI uses it in the no-Rust job to prove the differential still
	// runs, and it is the fastest way to check a recording is not stale.
	var bin string
	if os.Getenv("MJ_ORACLE_RECORDED_ONLY") == "" {
		bin = os.Getenv("MJ_ORACLE_HARNESS")
		if bin == "" {
			candidate := filepath.Join(root, DefaultHarnessBin)
			if _, err := os.Stat(candidate); err == nil {
				bin = candidate
			}
		}
	}

	key := fmt.Sprintf("%s\x00%s\x00%s", bin, corpusPath, corpus.SHA256)
	cached, _ := harnessCache.LoadOrStore(key, &harnessCacheEntry{})
	entry := cached.(*harnessCacheEntry)
	entry.once.Do(func() {
		entry.output, entry.source, entry.err = runHarnessOutcomes(root, corpus, bin)
	})
	return entry.output, entry.source, entry.err
}

// runHarnessOutcomes is the uncached body of [LoadHarnessOutcomes]: it produces
// the Rust-side outcomes and checks them against the corpus they claim to
// describe.
func runHarnessOutcomes(root string, corpus *Corpus, bin string) (*HarnessOutput, Source, error) {
	var raw []byte
	source := SourceRecorded
	if bin != "" {
		cmd := exec.Command(bin, corpus.Path)
		out, err := cmd.Output()
		if err != nil {
			return nil, "", fmt.Errorf("running harness %s: %w", bin, err)
		}
		raw = out
		source = SourceLive
	} else {
		out, err := os.ReadFile(RecordingPath(root, corpus))
		if err != nil {
			return nil, "", fmt.Errorf("no harness binary and no recording: %w", err)
		}
		raw = out
	}

	parsed, err := ParseHarnessOutput(raw)
	if err != nil {
		return nil, "", fmt.Errorf("harness output (%s) for corpus %s: %w", source, corpus.Name, err)
	}
	if parsed.Provenance.CorpusSHA256 != corpus.SHA256 {
		return nil, "", fmt.Errorf(
			"harness output (%s) was produced from corpus sha256 %s but %s is %s; re-record with oracle/record.sh",
			source, parsed.Provenance.CorpusSHA256, corpus.Path, corpus.SHA256)
	}
	return parsed, source, nil
}

// Run executes the whole differential: every corpus row through the fork,
// compared against the Rust engine's outcome, classified against the ledger.
func Run(root string) (*Report, error) {
	corpora, err := LoadCorpora(filepath.Join(root, CorpusDir))
	if err != nil {
		return nil, err
	}
	ledger, err := LoadLedger(filepath.Join(root, DefaultLedgerPath))
	if err != nil {
		return nil, err
	}

	report := &Report{}
	declared := make(map[string]bool)

	for _, corpus := range corpora {
		harness, source, err := LoadHarnessOutcomes(root, corpus)
		if err != nil {
			return nil, err
		}
		report.Runs = append(report.Runs, CorpusRun{
			Corpus:     corpus,
			Source:     source,
			Provenance: harness.Provenance,
		})

		for _, row := range corpus.Rows {
			rust, ok := harness.Lookup(row.ID)
			if !ok {
				report.Results = append(report.Results, RowResult{
					Corpus:  corpus.Name,
					Row:     row,
					Verdict: VerdictMissing,
					Note:    "the Rust harness produced no result for this row",
				})
				continue
			}
			got := RunFork(row)
			res := RowResult{Corpus: corpus.Name, Row: row, Rust: rust, Go: got}
			entry, hasEntry := ledger.Lookup(row.ID)
			if hasEntry {
				declared[row.ID] = true
			}

			switch {
			case rust.Equivalent(got):
				res.Verdict = VerdictMatch
				if hasEntry {
					res.Verdict = VerdictLedgerStale
					res.Class = entry.Class
					res.Note = "declared as a divergence but the engines now agree; remove the ledger entry"
				}
			case !hasEntry:
				res.Verdict = VerdictNewDivergence
				res.Class = classify(rust, got)
				res.Note = "undeclared divergence"
			case !entry.Accepts(rust.Signature(), got.Signature()):
				res.Verdict = VerdictLedgerStale
				res.Class = entry.Class
				res.Note = fmt.Sprintf(
					"divergence changed shape; ledger accepts rust=%v go=%v, run produced rust=%q go=%q",
					entry.RustSignatures, entry.GoSignatures, rust.Signature(), got.Signature())
			default:
				res.Verdict = VerdictKnownDivergence
				res.Class = entry.Class
				res.Note = entry.Summary
			}
			report.Results = append(report.Results, res)
		}
	}

	// A ledger entry with no corresponding corpus row is also stale: the
	// regression it names is no longer being run.
	for _, entry := range ledger.Entries {
		if declared[entry.ID] {
			continue
		}
		report.Results = append(report.Results, RowResult{
			Row:     Row{ID: entry.ID},
			Verdict: VerdictLedgerStale,
			Class:   entry.Class,
			Note:    "ledger entry has no corpus row; the regression it names is not being run",
		})
	}

	return report, nil
}

// classify gives an undeclared divergence a provisional label. Only the
// harness-incomplete case can be decided automatically; everything else is an
// engine difference until a later slice's BAML lane can say otherwise.
func classify(rust, got Outcome) Class {
	if rust.Status == StatusUnsupported || got.Status == StatusUnsupported {
		return ClassHarnessIncomplete
	}
	return ClassEngine
}

// ModuleRoot returns the oracle module root, derived from this source file's
// location so tests and the CLI agree regardless of working directory.
// MJ_ORACLE_ROOT overrides it.
func ModuleRoot() (string, error) {
	if root := os.Getenv("MJ_ORACLE_ROOT"); root != "" {
		return root, nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot determine oracle module root; set MJ_ORACLE_ROOT")
	}
	return filepath.Dir(file), nil
}
