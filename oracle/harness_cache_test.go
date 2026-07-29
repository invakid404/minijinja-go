package oracle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The harness memo exists so `go test ./...` evaluates each corpus once instead
// of four times (see [harnessCache]). What it must never do is answer for an
// engine that is no longer on disk.
//
// Both inputs to a differential run can change while keeping their name. A
// harness is rebuilt at the same path — that is what `cargo build --release`
// does, every time. A recording is re-recorded in place — that is what
// oracle/record.sh does. A memo keyed by pathname cannot see either, so a
// long-lived process that rebuilds its Rust reference and compares again would
// be told the old outcomes, and the corpus would go green against an engine
// that was replaced. These tests are the statement that it re-keys instead:
// each one replaces exactly one input's CONTENT, holds every name fixed, and
// requires the second load to observe the replacement.
//
// The pathname-keyed memo fails both of them.

// cacheProbeRow is the id the fake harness and recordings below answer for. It
// lives only in this file's temporary corpora, never in oracle/corpus.
const cacheProbeRow = "cacheprobe/row"

func TestHarnessCacheRekeysWhenTheBinaryIsReplacedInPlace(t *testing.T) {
	requirePOSIXShell(t)

	root := t.TempDir()
	corpus := writeProbeCorpus(t, root)
	bin := filepath.Join(root, "mj-oracle-harness")
	runs := filepath.Join(root, "runs")

	t.Setenv("MJ_ORACLE_RECORDED_ONLY", "")
	t.Setenv("MJ_ORACLE_HARNESS", bin)

	writeFakeHarness(t, bin, runs, "first binary", corpus.SHA256)
	if got, source := loadProbe(t, root, corpus); got != "first binary" || source != SourceLive {
		t.Fatalf("first load = %q (%s), want %q (%s)", got, source, "first binary", SourceLive)
	}

	// The memo's whole purpose: an unchanged binary over an unchanged corpus is
	// executed once, however many times it is asked for.
	if got, _ := loadProbe(t, root, corpus); got != "first binary" {
		t.Fatalf("repeat load = %q, want %q", got, "first binary")
	}
	if n := countRuns(t, runs); n != 1 {
		t.Errorf("harness ran %d times for an unchanged binary and corpus, want 1: "+
			"the memo is not memoizing, which is what it exists to do", n)
	}

	// Same pathname, same corpus, different executable — a rebuild.
	writeFakeHarness(t, bin, runs, "second binary", corpus.SHA256)
	got, source := loadProbe(t, root, corpus)
	if got != "second binary" {
		t.Errorf("after replacing the harness at %s, load = %q, want %q\n"+
			"  The cache served an outcome from the previous executable. A memo "+
			"keyed by pathname cannot tell a rebuilt harness from the one it "+
			"cached, so the differential would compare the fork against an "+
			"engine that is no longer on disk.", bin, got, "second binary")
	}
	if source != SourceLive {
		t.Errorf("source = %s, want %s", source, SourceLive)
	}
	if n := countRuns(t, runs); n != 2 {
		t.Errorf("harness ran %d times across a rebuild, want 2", n)
	}
}

// A harness name does not have to contain a directory. MJ_ORACLE_HARNESS is
// whatever the caller exported, and a bare `mj-oracle-harness` found on PATH is
// an ordinary way to say it. That spelling is where the two halves of the memo
// can come apart: exec.Command resolves a name with no separator through PATH,
// while the digest's os.Open resolves the same name against the working
// directory. The next two tests are the statement that both halves mean the
// same file however the name was written — the first that an unchanged one is
// still executed once, the second that replacing the executable that actually
// runs is still seen.

func TestHarnessCacheMemoizesAPathResolvedHarness(t *testing.T) {
	requirePOSIXShell(t)

	root := t.TempDir()
	corpus := writeProbeCorpus(t, root)
	binDir := t.TempDir()
	runs := filepath.Join(root, "runs")
	const name = "mj-oracle-harness-path-probe"

	// Nothing of that name in the working directory — the executable exists
	// only on PATH, which is what a bare name usually means.
	chdir(t, t.TempDir())
	prependToPATH(t, binDir)
	t.Setenv("MJ_ORACLE_RECORDED_ONLY", "")
	t.Setenv("MJ_ORACLE_HARNESS", name)

	writeFakeHarness(t, filepath.Join(binDir, name), runs, "path harness", corpus.SHA256)

	for i := 1; i <= 2; i++ {
		if got, source := loadProbe(t, root, corpus); got != "path harness" || source != SourceLive {
			t.Fatalf("load %d = %q (%s), want %q (%s)", i, got, source, "path harness", SourceLive)
		}
	}
	if n := countRuns(t, runs); n != 1 {
		t.Errorf("PATH-resolved harness ran %d times for an unchanged binary and corpus, want 1\n"+
			"  Both loads went uncached, which is the memo not memoizing — its "+
			"whole reason to exist. The key was digested from the working "+
			"directory's %q, which is not there, while exec ran the one on PATH.",
			n, name)
	}
}

func TestHarnessCacheRekeysWhenThePathResolvedBinaryIsReplacedInPlace(t *testing.T) {
	requirePOSIXShell(t)

	root := t.TempDir()
	corpus := writeProbeCorpus(t, root)
	binDir := t.TempDir()
	cwd := t.TempDir()
	runs := filepath.Join(root, "runs")
	const name = "mj-oracle-harness-path-probe"

	// A same-named file in the working directory that is not the harness: not
	// executable, never rebuilt, and not what PATH points at. os.Open finds it,
	// exec never will, and it is held fixed for the whole test — so a key
	// digested from it cannot move, whatever happens to the real executable.
	decoy := filepath.Join(cwd, name)
	if err := os.WriteFile(decoy, []byte("not the harness: never executed, never changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, cwd)
	prependToPATH(t, binDir)
	t.Setenv("MJ_ORACLE_RECORDED_ONLY", "")
	t.Setenv("MJ_ORACLE_HARNESS", name)

	bin := filepath.Join(binDir, name)
	writeFakeHarness(t, bin, runs, "first actual binary", corpus.SHA256)
	if got, source := loadProbe(t, root, corpus); got != "first actual binary" || source != SourceLive {
		t.Fatalf("first load = %q (%s), want %q (%s)", got, source, "first actual binary", SourceLive)
	}

	// Rebuild the executable that actually runs, leaving the decoy alone.
	writeFakeHarness(t, bin, runs, "second actual binary", corpus.SHA256)
	got, source := loadProbe(t, root, corpus)
	if got != "second actual binary" {
		t.Errorf("after replacing the PATH-resolved harness at %s, load = %q, want %q\n"+
			"  The cache served an outcome from the previous executable. The key "+
			"described ./%s, which never changed, while the differential ran the "+
			"rebuilt file on PATH: a comparison against an engine that is no "+
			"longer on disk, under a key that looked stable because it was "+
			"describing the wrong file.", bin, got, "second actual binary", name)
	}
	if source != SourceLive {
		t.Errorf("source = %s, want %s", source, SourceLive)
	}
	if n := countRuns(t, runs); n != 2 {
		t.Errorf("harness ran %d times across a rebuild, want 2", n)
	}
}

func TestHarnessCacheRekeysWhenTheRecordingIsReplacedInPlace(t *testing.T) {
	root := t.TempDir()
	corpus := writeProbeCorpus(t, root)

	// Force the replay path, exactly as the no-Rust CI job does.
	t.Setenv("MJ_ORACLE_RECORDED_ONLY", "1")
	t.Setenv("MJ_ORACLE_HARNESS", "")

	writeRecording(t, root, corpus, "first recording")
	if got, source := loadProbe(t, root, corpus); got != "first recording" || source != SourceRecorded {
		t.Fatalf("first load = %q (%s), want %q (%s)", got, source, "first recording", SourceRecorded)
	}

	// Same recording path, same corpus, different bytes — a re-record.
	writeRecording(t, root, corpus, "second recording")
	got, source := loadProbe(t, root, corpus)
	if got != "second recording" {
		t.Errorf("after re-recording %s, load = %q, want %q\n"+
			"  The cache served the previously parsed recording. Recordings are "+
			"rewritten in place by oracle/record.sh, so a memo that cannot see "+
			"new bytes under an old name replays a retired reference.",
			RecordingPath(root, corpus), got, "second recording")
	}
	if source != SourceRecorded {
		t.Errorf("source = %s, want %s", source, SourceRecorded)
	}
}

// TestHarnessCacheDoesNotSwallowAMissingSource covers the other half of the
// identity rule: when the outcome source cannot be digested at all there is no
// honest key for it, so nothing is cached and the failure is reported by the
// load path in its own words rather than being frozen into the memo.
func TestHarnessCacheDoesNotSwallowAMissingSource(t *testing.T) {
	root := t.TempDir()
	corpus := writeProbeCorpus(t, root)

	t.Run("missing binary", func(t *testing.T) {
		t.Setenv("MJ_ORACLE_RECORDED_ONLY", "")
		t.Setenv("MJ_ORACLE_HARNESS", filepath.Join(root, "not-a-harness"))

		_, _, err := LoadHarnessOutcomes(root, corpus)
		if err == nil || !strings.Contains(err.Error(), "running harness") {
			t.Fatalf("err = %v, want a 'running harness' failure", err)
		}
	})

	t.Run("unresolvable bare name", func(t *testing.T) {
		requirePOSIXShell(t)

		binDir := t.TempDir()
		runs := filepath.Join(root, "runs-unresolvable")
		const name = "mj-oracle-harness-absent-probe"

		// Nowhere on PATH and nowhere in the working directory. A name that
		// resolves to no file cannot be digested, so it gets no entry, and the
		// failure is reported under the name that was actually configured
		// rather than under whatever resolution guessed at.
		chdir(t, t.TempDir())
		prependToPATH(t, binDir)
		t.Setenv("MJ_ORACLE_RECORDED_ONLY", "")
		t.Setenv("MJ_ORACLE_HARNESS", name)

		_, _, err := LoadHarnessOutcomes(root, corpus)
		if err == nil || !strings.Contains(err.Error(), "running harness "+name) {
			t.Fatalf("err = %v, want a 'running harness %s' failure", err, name)
		}

		// And an executable that appears on PATH afterwards is picked up: an
		// unresolvable name was never given a cache entry to be stuck in.
		writeFakeHarness(t, filepath.Join(binDir, name), runs, "arrived on PATH", corpus.SHA256)
		if got, _ := loadProbe(t, root, corpus); got != "arrived on PATH" {
			t.Errorf("load after the harness appeared on PATH = %q, want %q", got, "arrived on PATH")
		}
	})

	t.Run("missing recording", func(t *testing.T) {
		t.Setenv("MJ_ORACLE_RECORDED_ONLY", "1")
		t.Setenv("MJ_ORACLE_HARNESS", "")

		_, _, err := LoadHarnessOutcomes(root, corpus)
		if err == nil || !strings.Contains(err.Error(), "no harness binary and no recording") {
			t.Fatalf("err = %v, want a missing-recording failure", err)
		}

		// And once the recording appears, the same process picks it up: an
		// absent source was never given a cache entry to be stuck in.
		writeRecording(t, root, corpus, "arrived late")
		if got, _ := loadProbe(t, root, corpus); got != "arrived late" {
			t.Errorf("load after the recording appeared = %q, want %q", got, "arrived late")
		}
	})
}

// loadProbe loads the outcomes for a probe corpus and returns the render of its
// single row.
func loadProbe(t *testing.T, root string, corpus *Corpus) (string, Source) {
	t.Helper()
	out, source, err := LoadHarnessOutcomes(root, corpus)
	if err != nil {
		t.Fatalf("LoadHarnessOutcomes: %v", err)
	}
	outcome, ok := out.Lookup(cacheProbeRow)
	if !ok {
		t.Fatalf("harness output has no row %q", cacheProbeRow)
	}
	return outcome.Render, source
}

// writeProbeCorpus writes a one-row corpus under root and loads it, so the
// returned corpus carries the digest the harness document has to claim.
func writeProbeCorpus(t *testing.T, root string) *Corpus {
	t.Helper()
	dir := filepath.Join(root, CorpusDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cacheprobe.json")
	body := fmt.Sprintf(
		`{"schema_version":%d,"rows":[{"id":%q,"surface":"arithmetic","form":"expression","source":"1 + 1","expect":"bytes"}]}`,
		SchemaVersion, cacheProbeRow)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	corpus, err := LoadCorpus(path)
	if err != nil {
		t.Fatal(err)
	}
	return corpus
}

// harnessDoc is a minimal harness document for the probe corpus, rendering the
// given value for its single row.
func harnessDoc(t *testing.T, render, corpusSHA string) []byte {
	t.Helper()
	raw, err := json.Marshal(HarnessOutput{
		SchemaVersion: SchemaVersion,
		Provenance: Provenance{
			EngineRepo:     "https://github.com/boundaryml/minijinja",
			EngineBranch:   "value-cmp",
			EngineRev:      "8cfc770a5dffeda2de5b910d2b9f870d7edeff7c",
			HarnessVersion: "1",
			OS:             runtime.GOOS,
			Arch:           runtime.GOARCH,
			CorpusSHA256:   corpusSHA,
		},
		Results: []HarnessResult{{
			ID:      cacheProbeRow,
			Outcome: Outcome{Status: StatusOK, Render: render},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// writeFakeHarness installs an executable stand-in for the Rust harness at
// path: it appends one byte to runs so invocations can be counted, then prints
// a fixed document. Calling it again with a different render replaces the
// executable's bytes while leaving its name alone — a rebuild in place.
func writeFakeHarness(t *testing.T, path, runs, render, corpusSHA string) {
	t.Helper()
	if strings.ContainsAny(runs, `'`+"\n") {
		t.Fatalf("run-counter path %q cannot be quoted for /bin/sh", runs)
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf x >> '%s'\ncat <<'MJ_FAKE_HARNESS_EOF'\n%s\nMJ_FAKE_HARNESS_EOF\n",
		runs, harnessDoc(t, render, corpusSHA))
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// WriteFile keeps the existing mode when it overwrites, so set it outright.
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeRecording installs a committed-style recording for the probe corpus at
// the path the replay path reads.
func writeRecording(t *testing.T, root string, corpus *Corpus, render string) {
	t.Helper()
	path := RecordingPath(root, corpus)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, harnessDoc(t, render, corpus.SHA256), 0o644); err != nil {
		t.Fatal(err)
	}
}

// countRuns is how many times the fake harness has been executed.
func countRuns(t *testing.T, runs string) int {
	t.Helper()
	raw, err := os.ReadFile(runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(raw)
}

// prependToPATH puts dir first on PATH for the length of the test, so a bare
// harness name resolves to what dir holds.
func prependToPATH(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// chdir moves the process into dir for the length of the test.
//
// The working directory is half of what a bare harness name means — os.Open
// reads it there, exec does not — so the PATH-resolution tests have to control
// it to say anything. Nothing in this package runs with t.Parallel, so a
// process-wide directory held for one test is not visible to another.
// (testing.T.Chdir is this in one line, and needs go1.24; the module is 1.23.)
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Errorf("restoring working directory %s: %v", prev, err)
		}
	})
}

// requirePOSIXShell skips the live-harness probe where /bin/sh is not the way
// to write a throwaway executable. The oracle module is only ever exercised on
// the differential's two CI architectures, both POSIX.
func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake harness is a /bin/sh script")
	}
}
