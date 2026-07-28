// Command oracle runs the differential between this Go fork and BAML's pinned
// Rust minijinja engine, or re-records the Rust side.
//
//	go run ./cmd/oracle report   # run the differential and print a table
//	go run ./cmd/oracle record   # rebuild the Rust harness and refresh the recording
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/invakid404/minijinja-go/oracle"
)

func main() {
	cmd := "report"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	root, err := oracle.ModuleRoot()
	if err != nil {
		fail(err)
	}
	switch cmd {
	case "report":
		os.Exit(report(root))
	case "record":
		if err := record(root); err != nil {
			fail(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "usage: oracle [report|record]\n")
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "oracle: %v\n", err)
	os.Exit(1)
}

func report(root string) int {
	rep, err := oracle.Run(root)
	if err != nil {
		fail(err)
	}

	p := rep.Provenance
	fmt.Printf("rust engine : %s@%s (branch %s)\n", p.EngineRepo, p.EngineRev, p.EngineBranch)
	fmt.Printf("features    : %s\n", strings.Join(p.EngineFeatures, ","))
	fmt.Printf("platform    : %s/%s   outcomes: %s\n", p.OS, p.Arch, rep.Source)
	fmt.Printf("corpus      : %d rows, sha256 %s\n\n", len(rep.Corpus.Rows), rep.Corpus.SHA256)

	width := 0
	for _, res := range rep.Results {
		if len(res.Row.ID) > width {
			width = len(res.Row.ID)
		}
	}

	for _, res := range rep.Results {
		mark := "ok  "
		switch res.Verdict {
		case oracle.VerdictKnownDivergence:
			mark = "DIFF"
		case oracle.VerdictNewDivergence, oracle.VerdictLedgerStale, oracle.VerdictMissing:
			mark = "FAIL"
		}
		fmt.Printf("%s  %-*s  %s\n", mark, width, res.Row.ID, res.Verdict)
		if res.Verdict == oracle.VerdictMatch {
			continue
		}
		if res.Class != "" {
			fmt.Printf("        class : %s\n", res.Class)
		}
		if res.Note != "" {
			fmt.Printf("        note  : %s\n", res.Note)
		}
		fmt.Printf("        source: %s\n", res.Row.TemplateSource())
		fmt.Printf("        rust  : %s\n", res.Rust.Describe())
		fmt.Printf("        go    : %s\n", res.Go.Describe())
	}

	counts := rep.Counts()
	fmt.Printf("\n%d rows: %d match, %d known divergence, %d new divergence, %d stale ledger, %d missing\n",
		len(rep.Results),
		counts[oracle.VerdictMatch], counts[oracle.VerdictKnownDivergence],
		counts[oracle.VerdictNewDivergence], counts[oracle.VerdictLedgerStale],
		counts[oracle.VerdictMissing])

	byClass := map[oracle.Class]int{}
	for _, res := range rep.Results {
		if res.Verdict == oracle.VerdictKnownDivergence {
			byClass[res.Class]++
		}
	}
	if len(byClass) > 0 {
		fmt.Printf("known divergences by class:")
		for class, n := range byClass {
			fmt.Printf(" %s=%d", class, n)
		}
		fmt.Println()
	}

	if len(rep.Failures()) > 0 {
		return 1
	}
	return 0
}

// record rebuilds the Rust harness and refreshes the committed recording, so
// the differential can run without a Rust toolchain while still comparing
// against real engine output.
func record(root string) error {
	harnessDir := filepath.Join(root, "harness")
	build := exec.Command("cargo", "build", "--release")
	build.Dir = harnessDir
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("building harness: %w", err)
	}

	bin := filepath.Join(harnessDir, "target", "release", "mj-oracle-harness")
	corpusPath := filepath.Join(root, oracle.DefaultCorpusPath)
	run := exec.Command(bin, corpusPath)
	run.Stderr = os.Stderr
	raw, err := run.Output()
	if err != nil {
		return fmt.Errorf("running harness: %w", err)
	}

	// Parse before writing: a recording that cannot be replayed is worse than
	// no recording.
	parsed, err := oracle.ParseHarnessOutput(raw)
	if err != nil {
		return fmt.Errorf("harness produced unusable output: %w", err)
	}
	corpus, err := oracle.LoadCorpus(corpusPath)
	if err != nil {
		return err
	}
	if parsed.Provenance.CorpusSHA256 != corpus.SHA256 {
		return fmt.Errorf("harness recorded corpus sha256 %s but corpus is %s",
			parsed.Provenance.CorpusSHA256, corpus.SHA256)
	}

	out := filepath.Join(root, oracle.DefaultRecordedPath)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if !json.Valid(raw) {
		return fmt.Errorf("harness output is not valid JSON")
	}
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "recorded %d outcomes from %s@%s (%s/%s) to %s\n",
		len(parsed.Results), parsed.Provenance.EngineRepo, parsed.Provenance.EngineRev[:7],
		parsed.Provenance.OS, parsed.Provenance.Arch, out)
	return nil
}
