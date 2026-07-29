package oracle

import "testing"

// Panic rows, and the one field the byte-exact contract deliberately does not
// cover.
//
// # The contract
//
// A panic is an outcome, and `Outcome.Signature` reduces it to `"panic"`. That
// is deliberate, and it is the whole of what the differential compares here:
// the semantic outcome of a panic row is "evaluation aborted at this input",
// which both engines either do or do not do. A panic has no render, no boolean
// and no error category.
//
// The panic DIAGNOSTIC is out of that contract, because it is not produced by
// either engine. It is the host language's runtime message for the fault:
//
//	tmpl/loop-cycle-no-args   Rust: "attempt to calculate the remainder with a divisor of zero"
//	                          Go:   "runtime error: integer divide by zero"
//
// Those are std's messages for the same arithmetic fault, and the template
// slice shipped that row knowing it. The numeric rows are the same situation one
// step further: Rust's text names `Option::unwrap()`, an API this fork does not
// have, so emitting it verbatim would be a Go library reporting a Rust
// diagnostic — less useful to a caller that recovers, and no more faithful,
// since the fault is the thing being reproduced and the fault IS reproduced.
// The fork's own diagnostic names the operands instead, and its typed
// `value.UncomparableNumbers` carries them.
//
// # Why "out of contract" is not "unchecked"
//
// A field that is neither compared nor pinned is a field where either side can
// drift silently, which would let a green gate mean less than it says. So both
// diagnostics are PINNED here instead, from both sides, together with the
// status parity that IS the contract. The result:
//
//   - a row that starts or stops panicking on either side fails;
//   - a change to the RECORDED RUST diagnostic fails;
//   - a change to the fork's diagnostic fails;
//   - a new panic row anywhere in the corpus fails until it is added here.
//
// So "the numeric class is byte-exact" means byte-exact on the compared
// signature, with the one field outside it pinned rather than ignored.

// panicRows is every corpus row on which either engine faults, with both
// diagnostics recorded verbatim. Adding a row here is a deliberate act: an
// input that aborts evaluation is worth someone's attention.
var panicRows = map[string]struct{ rust, fork string }{
	// The engine's `Ord` handles a failed lossless coercion by assuming both
	// operands are objects and unwrapping `self.as_object()`; for two numbers
	// that is a None (value/mod.rs:638-641). PATCHES.md #30.
	"num/lt-int-float-lossy": {
		rust: "called `Option::unwrap()` on a `None` value",
		fork: "cannot order 9007199254740993 against 1.5: neither converts to the other without loss",
	},
	"num/gt-float-int-lossy": {
		rust: "called `Option::unwrap()` on a `None` value",
		fork: "cannot order 1.5 against 9007199254740993: neither converts to the other without loss",
	},
	"num/lt-i64max-vs-2pow63-float": {
		rust: "called `Option::unwrap()` on a `None` value",
		fork: "cannot order 9223372036854775807 against 9223372036854776000.0: neither converts to the other without loss",
	},
	"num/lt-u128-vs-int": {
		rust: "called `Option::unwrap()` on a `None` value",
		fork: "cannot order 340282366920938463463374607431768211455 against 1: neither converts to the other without loss",
	},
	// The same fault reached through a builtin rather than an operator: sort
	// orders with the same comparison.
	"num/sort-uncomparable": {
		rust: "called `Option::unwrap()` on a `None` value",
		fork: "cannot order 1.5 against 9007199254740993: neither converts to the other without loss",
	},
	// The template slice's row: `loop.cycle()` computes `idx % args.len()`
	// before inspecting the argument list. PATCHES.md #9.
	"tmpl/loop-cycle-no-args": {
		rust: "attempt to calculate the remainder with a divisor of zero",
		fork: "runtime error: integer divide by zero",
	},
}

// panicDeclines are the rows where the ENGINE faults and this fork
// deliberately does not. Refusing is the safe direction — answering where the
// reference aborts is the dangerous one — but it is still a divergence, so
// every entry here must also be declared in `oracle/divergences.json`, which
// is what the differential enforces. The engine's diagnostic is pinned all the
// same, because a change to it can mean a different fault.
var panicDeclines = map[string]struct{ rust, reason string }{
	// `a % b` on integers with a zero divisor (tests.rs:153-159). The fork
	// returns an invalid operation: reproducing a panic in a library is not
	// acceptable, and answering `false` where the reference aborts is worse.
	// PATCHES.md #56.
	"test/divisibleby-zero": {
		rust:   "attempt to calculate the remainder with a divisor of zero",
		reason: "deliberate and permanent; see divergences.json",
	},
	// A `usize` argument that sizes an allocation. The conversion succeeds on
	// both sides — a usize is a u64 on a 64-bit target — and the engine then
	// reserves that much memory and aborts. The fork refuses instead, for the
	// same reason it refuses a zero divisor. PATCHES.md #82.
	"review/usize-batch-u64-upper":       usizeAllocDecline,
	"review/usize-slice-u64-upper":       usizeAllocDecline,
	"review/usize-indent-u64-upper":      usizeAllocDecline,
	"review/usize-tojson-u64-upper":      usizeAllocDecline,
	"review/usize-batch-u64-max":         usizeAllocDecline,
	"review/usize-batch-float-u64-upper": usizeAllocDecline,
	"review/usize-batch-i64-max":         usizeAllocDecline,
}

// usizeAllocDecline is the engine's diagnostic for every one of those rows:
// `Vec::with_capacity` and `str::repeat` raise the same fault.
var usizeAllocDecline = struct{ rust, reason string }{
	rust:   "capacity overflow",
	reason: "deliberate and permanent; see divergences.json",
}

func TestPanicRowsAgreeOnStatusAndPinTheirDiagnostics(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadCorpora(root + "/" + CorpusDir)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
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
			want, declared := panicRows[row.ID]

			if rust.Status != StatusPanic && got.Status != StatusPanic {
				if declared {
					t.Errorf("%s: neither engine panics any more; remove it from panicRows", row.ID)
				}
				continue
			}
			seen[row.ID] = true

			// A row the fork deliberately declines to reproduce: the engine
			// faults, the fork errors, and the ledger carries it.
			if decline, isDecline := panicDeclines[row.ID]; isDecline {
				if declared {
					t.Errorf("%s: listed in both panicRows and panicDeclines", row.ID)
					continue
				}
				if rust.Status != StatusPanic {
					t.Errorf("%s: the engine no longer faults; remove it from panicDeclines\n  rust: %s",
						row.ID, rust.Describe())
					continue
				}
				if got.Status == StatusPanic {
					t.Errorf("%s: the fork faults too, so it is not a decline; move it to panicRows\n  go: %s",
						row.ID, got.Describe())
					continue
				}
				if got.Status != StatusError {
					t.Errorf("%s: a declined panic must be an ERROR, not %s (%s)\n  go: %s",
						row.ID, got.Status, decline.reason, got.Describe())
				}
				if rust.Message != decline.rust {
					t.Errorf("%s: the ENGINE's panic diagnostic changed\n  was:  %q\n  now:  %q\n"+
						"  Re-examine the fault before repinning: a different diagnostic can mean a different fault.",
						row.ID, decline.rust, rust.Message)
				}
				continue
			}

			if !declared {
				t.Errorf("%s: a panic row that is not pinned\n  rust: %s\n  go:   %s\n"+
					"  An input that aborts evaluation must be added to panicRows deliberately.",
					row.ID, rust.Describe(), got.Describe())
				continue
			}

			// Status parity IS the contract, and it is checked here as well as
			// by the differential, so this test alone would catch one side
			// ceasing to fault.
			if rust.Status != StatusPanic || got.Status != StatusPanic {
				t.Errorf("%s: only one engine faults\n  rust: %s\n  go:   %s",
					row.ID, rust.Describe(), got.Describe())
				continue
			}

			// The diagnostics are out of contract, so they are pinned rather
			// than compared against each other.
			if rust.Message != want.rust {
				t.Errorf("%s: the ENGINE's panic diagnostic changed\n  was:  %q\n  now:  %q\n"+
					"  Re-examine the fault before repinning: a different diagnostic can mean a different fault.",
					row.ID, want.rust, rust.Message)
			}
			if got.Message != want.fork {
				t.Errorf("%s: the FORK's panic diagnostic changed\n  was:  %q\n  now:  %q",
					row.ID, want.fork, got.Message)
			}
		}
	}

	for id := range panicRows {
		if !seen[id] {
			t.Errorf("panicRows pins %q, but no corpus row produced a panic for it", id)
		}
	}
	for id := range panicDeclines {
		if !seen[id] {
			t.Errorf("panicDeclines pins %q, but no corpus row produced a panic for it", id)
		}
	}
	if len(seen) == 0 {
		t.Error("no panic rows found at all; this test would be vacuous")
	}
}
