package oracle

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

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
//
// # One diagnostic is pinned by content rather than by wording
//
// Four rows are an exception to "the RECORDED RUST diagnostic", and it is an
// exception about the TOOLCHAIN, not about the contract. Rust std's wording
// for a not-a-char-boundary abort is not a stable API and differs between
// rustc releases, so pinning it verbatim pins whichever toolchain recorded it
// — and a CI runner on a different rustc then fails on a message that
// describes the same fault. Those rows pin the fault's content instead: the
// offset, the scalar and its byte range, all of which still fail on a change.
// See [charBoundaryAbort]. Every other diagnostic here is a message std has
// kept stable and is pinned verbatim.

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

// panicDecline is one such row's pin.
//
// Two of the fields are alternatives, and which one a row uses is a statement
// about the DIAGNOSTIC, not about the row: `rust` pins the wording verbatim,
// and `core` pins the fault's stable content for a diagnostic whose wording
// std does not keep stable across toolchains. Exactly one is set.
type panicDecline struct {
	// rust is the engine's diagnostic, pinned verbatim.
	rust string
	// core is the stable core of a not-a-char-boundary abort, pinned INSTEAD
	// of the wording. See [charBoundaryAbort].
	core *charBoundaryAbort
	// reason is why the fork declines to reproduce the fault.
	reason string
}

// charBoundaryAbort is the content of Rust's not-a-char-boundary abort, apart
// from how std happens to word it: the byte offset a slice was taken at, and
// the scalar that offset falls inside with its own byte range.
//
// std's wording for this fault is NOT a stable API and has changed between
// rustc releases. Two forms it has emitted for the same slice:
//
//	byte index 1 is not a char boundary; it is inside '日' (bytes 0..3) of `日`
//	end byte index 1 is not a char boundary; it is inside '日' (bytes 0..3 of string)
//
// They differ in the leading noun and in whether the string is echoed, and
// neither difference is a difference in the fault. Pinning either one verbatim
// pins the toolchain that produced it, which is what
// TestPanicRowsAgreeOnStatusAndPinTheirDiagnostics did until it went red on a
// CI runner whose rustc words it the other way.
//
// So these rows pin the CONTENT instead — the same discipline the generated
// Unicode tables use, where the drift guard compares the data the toolchain
// carries and not the toolchain's own text. An offset that moves, a different
// scalar, or a different byte range still fails, because those ARE the fault.
// This is deliberately narrow: it applies only to this one std message, and
// every other pinned diagnostic here stays verbatim.
type charBoundaryAbort struct {
	// Index is the byte offset the slice was taken at.
	Index int
	// Scalar is the character that offset falls inside.
	Scalar rune
	// Start and End are that character's own byte range in the string.
	Start, End int
}

// charBoundaryAbortPattern reads the content out of either wording.
//
// It anchors on the two clauses both forms share — "byte index N is not a char
// boundary" (the "end " prefix, when present, sits in front of it) and "it is
// inside <char debug> (bytes A..B" — and stops before the part they disagree
// about.
var charBoundaryAbortPattern = regexp.MustCompile(
	`byte index (\d+) is not a char boundary; it is inside '(.*?)' \(bytes (\d+)\.\.(\d+)`)

// parseCharBoundaryAbort extracts the fault from an abort message. The second
// result is false when the message is not this fault at all, which is itself a
// failure worth reporting rather than passing over.
func parseCharBoundaryAbort(message string) (charBoundaryAbort, bool) {
	m := charBoundaryAbortPattern.FindStringSubmatch(message)
	if m == nil {
		return charBoundaryAbort{}, false
	}
	scalar, ok := decodeRustCharDebug(m[2])
	if !ok {
		return charBoundaryAbort{}, false
	}
	index, err := strconv.Atoi(m[1])
	if err != nil {
		return charBoundaryAbort{}, false
	}
	start, err := strconv.Atoi(m[3])
	if err != nil {
		return charBoundaryAbort{}, false
	}
	end, err := strconv.Atoi(m[4])
	if err != nil {
		return charBoundaryAbort{}, false
	}
	return charBoundaryAbort{Index: index, Scalar: scalar, Start: start, End: end}, true
}

// decodeRustCharDebug reads back one character from the body of Rust's
// `Debug for char`, so the pin holds the SCALAR rather than its rendering.
// A future toolchain that spells U+0301 `\u{0301}` instead of `\u{301}` is
// then not a failure, because it is not a different character.
func decodeRustCharDebug(body string) (rune, bool) {
	if strings.HasPrefix(body, `\u{`) && strings.HasSuffix(body, `}`) {
		n, err := strconv.ParseUint(body[3:len(body)-1], 16, 32)
		if err != nil || n > unicode.MaxRune {
			return 0, false
		}
		return rune(n), true
	}
	if len(body) == 2 && body[0] == '\\' {
		// The short escapes `char::escape_debug` can produce.
		switch body[1] {
		case 'n':
			return '\n', true
		case 'r':
			return '\r', true
		case 't':
			return '\t', true
		case '\\':
			return '\\', true
		case '\'':
			return '\'', true
		case '"':
			return '"', true
		case '0':
			return 0, true
		}
		return 0, false
	}
	r, size := utf8.DecodeRuneInString(body)
	if r == utf8.RuneError || size != len(body) {
		return 0, false
	}
	return r, true
}

// panicDeclines are the rows where the ENGINE faults and this fork
// deliberately does not. Refusing is the safe direction — answering where the
// reference aborts is the dangerous one — but it is still a divergence, so
// every entry here must also be declared in `oracle/divergences.json`, which
// is what the differential enforces. The engine's diagnostic is pinned all the
// same, because a change to it can mean a different fault.
var panicDeclines = map[string]panicDecline{
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
	// A format PRECISION is a byte count, and the engine applies it by slicing
	// the Rust string with it (format_utils.rs:227-231). A cut that is not a
	// UTF-8 character boundary aborts inside `str` indexing, so there is no
	// successful outcome to reproduce; Go's own slicing would return the
	// truncated encoding, which is not valid UTF-8 and is not what the engine
	// answers either. The fork refuses. PATCHES.md #89.
	//
	// These four pin the fault's CONTENT rather than std's wording for it, and
	// they are the only rows here that do; see [charBoundaryAbort] for why.
	// The cut is what distinguishes them, so each carries its own.
	"review/pyformat-precision-char-boundary": {
		core:   &charBoundaryAbort{Index: 1, Scalar: '\u65e5', Start: 0, End: 3},
		reason: "deliberate and permanent; see divergences.json",
	},
	"review/pyformat-precision-char-boundary-printf": {
		core:   &charBoundaryAbort{Index: 1, Scalar: '\u65e5', Start: 0, End: 3},
		reason: "deliberate and permanent; see divergences.json",
	},
	"review/pyformat-precision-char-boundary-combining": {
		core:   &charBoundaryAbort{Index: 2, Scalar: '\u0301', Start: 1, End: 3},
		reason: "deliberate and permanent; see divergences.json",
	},
	"review/pyformat-precision-char-boundary-mid-string": {
		core:   &charBoundaryAbort{Index: 4, Scalar: '\u672c', Start: 3, End: 6},
		reason: "deliberate and permanent; see divergences.json",
	},
}

// usizeAllocDecline is the engine's diagnostic for every one of those rows:
// `Vec::with_capacity` and `str::repeat` raise the same fault. Two words std
// has never reworded, so this one stays pinned verbatim.
var usizeAllocDecline = panicDecline{
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
				switch {
				case decline.core != nil:
					// std's wording for this fault is not stable across
					// toolchains, so the FAULT is pinned and the wording is
					// not. See [charBoundaryAbort].
					core, ok := parseCharBoundaryAbort(rust.Message)
					if !ok {
						t.Errorf("%s: the engine's abort is no longer a char-boundary fault\n  now:  %q\n"+
							"  Re-examine it before repinning: this row exists because the engine aborts HERE.",
							row.ID, rust.Message)
					} else if core != *decline.core {
						t.Errorf("%s: the engine aborts at a DIFFERENT cut\n  was:  %+v\n  now:  %+v\n  message: %q\n"+
							"  The offset, the scalar and its byte range are the fault; only std's wording is free to change.",
							row.ID, *decline.core, core, rust.Message)
					}
				case rust.Message != decline.rust:
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

// TestCharBoundaryAbortCoreIsIndependentOfRustcWording is the guard that this
// pin cannot be re-tightened onto a toolchain by accident.
//
// It carries BOTH wordings rustc has been observed to emit for these exact
// four rows — the local toolchain's and the CI runner's, which is what turned
// TestPanicRowsAgreeOnStatusAndPinTheirDiagnostics red — and asserts they read
// as the same fault. A machine can only ever produce one of them, so without
// this the other wording is untested until some future runner emits it.
func TestCharBoundaryAbortCoreIsIndependentOfRustcWording(t *testing.T) {
	// Keyed by the corpus row whose abort each pair of wordings is.
	wordings := map[string][2]string{
		"review/pyformat-precision-char-boundary": {
			"byte index 1 is not a char boundary; it is inside '日' (bytes 0..3) of `日`",
			"end byte index 1 is not a char boundary; it is inside '日' (bytes 0..3 of string)",
		},
		"review/pyformat-precision-char-boundary-printf": {
			"byte index 1 is not a char boundary; it is inside '日' (bytes 0..3) of `日`",
			"end byte index 1 is not a char boundary; it is inside '日' (bytes 0..3 of string)",
		},
		"review/pyformat-precision-char-boundary-combining": {
			"byte index 2 is not a char boundary; it is inside '\\u{301}' (bytes 1..3) of `é`",
			"end byte index 2 is not a char boundary; it is inside '\\u{301}' (bytes 1..3 of string)",
		},
		"review/pyformat-precision-char-boundary-mid-string": {
			"byte index 4 is not a char boundary; it is inside '本' (bytes 3..6) of `日本`",
			"end byte index 4 is not a char boundary; it is inside '本' (bytes 3..6 of string)",
		},
	}

	for id, pair := range wordings {
		decline, ok := panicDeclines[id]
		if !ok || decline.core == nil {
			t.Errorf("%s: no char-boundary core is pinned for it any more", id)
			continue
		}
		for _, message := range pair {
			core, ok := parseCharBoundaryAbort(message)
			if !ok {
				t.Errorf("%s: %q does not read as a char-boundary fault", id, message)
				continue
			}
			if core != *decline.core {
				t.Errorf("%s: %q reads as %+v, want %+v", id, message, core, *decline.core)
			}
		}
	}
}

// TestCharBoundaryAbortCoreStillFailsOnADifferentFault pins the other half:
// reading the content rather than the wording must not have made the pin
// vacuous. Everything that IS the fault still has to match.
func TestCharBoundaryAbortCoreStillFailsOnADifferentFault(t *testing.T) {
	want := charBoundaryAbort{Index: 1, Scalar: '日', Start: 0, End: 3}

	for _, tc := range []struct {
		name    string
		message string
		parses  bool
	}{
		{"the pinned fault, local wording",
			"byte index 1 is not a char boundary; it is inside '日' (bytes 0..3) of `日`", true},
		{"the pinned fault, CI wording",
			"end byte index 1 is not a char boundary; it is inside '日' (bytes 0..3 of string)", true},
		{"a cut at a different offset",
			"byte index 2 is not a char boundary; it is inside '日' (bytes 0..3) of `日`", true},
		{"a cut inside a different scalar",
			"byte index 1 is not a char boundary; it is inside '本' (bytes 0..3) of `本`", true},
		{"a scalar with a different byte range",
			"byte index 1 is not a char boundary; it is inside 'é' (bytes 0..2) of `é`", true},
		{"a different fault entirely", "capacity overflow", false},
		{"the fork ceasing to abort", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, ok := parseCharBoundaryAbort(tc.message)
			if ok != tc.parses {
				t.Fatalf("parsed = %v, want %v", ok, tc.parses)
			}
			// Only the first two are the pinned fault; every other line is a
			// real change and must be reported as one.
			isPinned := ok && core == want
			wantPinned := tc.name == "the pinned fault, local wording" ||
				tc.name == "the pinned fault, CI wording"
			if isPinned != wantPinned {
				t.Errorf("%q matched the pin = %v, want %v (read as %+v)",
					tc.message, isPinned, wantPinned, core)
			}
		})
	}
}

// TestPanicDeclinesPinExactlyOneForm keeps the two alternatives from drifting
// into each other: a row that pins neither is unpinned, and one that pins both
// hides whichever the test does not read.
func TestPanicDeclinesPinExactlyOneForm(t *testing.T) {
	for id, decline := range panicDeclines {
		switch {
		case decline.rust == "" && decline.core == nil:
			t.Errorf("%s: pins no diagnostic at all", id)
		case decline.rust != "" && decline.core != nil:
			t.Errorf("%s: pins both a verbatim wording and a fault core; only one is read", id)
		}
		if decline.reason == "" {
			t.Errorf("%s: declines to reproduce a fault without saying why", id)
		}
	}
}

// TestDecodeRustCharDebugReadsTheScalar covers the escape forms
// `Debug for char` can produce, so the pin holds a CHARACTER and not a
// spelling of one.
func TestDecodeRustCharDebugReadsTheScalar(t *testing.T) {
	for _, tc := range []struct {
		body string
		want rune
		ok   bool
	}{
		{"日", '日', true},
		{"a", 'a', true},
		{`\u{301}`, '́', true},
		{`\u{0301}`, '́', true}, // zero-padded: a spelling change, not a different scalar
		{`\u{10FFFF}`, '\U0010FFFF', true},
		{`\n`, '\n', true},
		{`\r`, '\r', true},
		{`\t`, '\t', true},
		{`\\`, '\\', true},
		{`\'`, '\'', true},
		{`\0`, 0, true},
		{`\u{110000}`, 0, false}, // past the last scalar value
		{`\u{}`, 0, false},
		{`\q`, 0, false},
		{"ab", 0, false},
		{"", 0, false},
	} {
		got, ok := decodeRustCharDebug(tc.body)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("decodeRustCharDebug(%q) = %q, %v; want %q, %v", tc.body, got, ok, tc.want, tc.ok)
		}
	}
}
