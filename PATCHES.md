# Intentional semantic deltas

Every deliberate behavioural difference between this fork and its upstream
baseline is recorded here, keyed by the differential corpus ID that pins it.

The baseline is `v2.16.0-baml.2` — upstream `mitsuhiko/minijinja@b9afca`
(`minijinja-go/v2.16.0`) with no semantic change at all, proven mechanically by
[`scripts/verify-seed.sh`](scripts/verify-seed.sh). Everything below is a delta
over that baseline, introduced by the template sweep (slice 6 of the scope's
plan) and the numeric core (slice 3), so that the fork matches BAML's engine
(`boundaryml/minijinja@8cfc770`, built with BAML's feature set).

## Rules

- **No entry, no patch.** A behavioural change that is not listed here is a bug,
  and `scripts/verify-seed.sh` will report it as an undocumented delta. Its
  `SEMANTIC_DELTA` list names the derived files these entries touch, and a file
  that stops differing fails the check too, so the declaration cannot rot.
- **Every entry cites a corpus ID.** A semantic claim without a differential row
  proving it is not parity work. The row must exist in `oracle/corpus/` and, if
  it is still red, in `oracle/divergences.json`.
- **Fixing a divergence removes its ledger entry.** The oracle fails on a ledger
  entry that no longer diverges, so closing a patch is a deliberate, reviewed
  act rather than a silent one.
- **Entries survive upstream merges.** Step 4 of the merge procedure re-applies
  this list one commit at a time.

## Log

| # | Corpus ID(s) | Area | Upstream behaviour | Fork behaviour | Rationale | Landed in |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | `err/syntax-incomplete-if`, and the whole `tmpl/err-*` family | Compile-error classification | `Parse` returns `*parser.Error`, a type in an internal package, so an external consumer cannot tell a syntax error from any other failure. | Every compile failure leaves the engine as `*minijinja.Error` of kind `ErrSyntax`, carrying the template name and the line/column the tokenizer or parser stopped at. `ParseDefault` keeps the internal shape for the snapshot tests. | BAML reports a template that does not compile as a located syntax error. The port made that outcome unclassifiable, which the differential recorded on 28 rows. | `feat/mjfork-template` |
| 2 | `tmpl/negative-include`, `tmpl/negative-include-ignore-missing`, `tmpl/negative-extends`, `tmpl/negative-import`, `tmpl/negative-from-import`, `tmpl/negative-block`, `tmpl/negative-block-super`, `tmpl/negative-self-block-call`, `tmpl/negative-break`, `tmpl/negative-continue`, `tmpl/negative-stray-endblock` | Engine feature set | `block`, `extends`, `include`, `import`, `from`, `break` and `continue` are statements, and `self` is bound to a block accessor. | None of them is a statement: the parser reports `unknown statement <name>` exactly as BAML's build does, and `self` is an ordinary undefined name. `internal/parser/features.go` is the single place that decides it. | BAML builds minijinja with `default-features = false` and without `multi_template`, `loader` or `loop_controls` (`BoundaryML/baml@85247f45 engine/Cargo.toml:99-115`). In Rust those keywords are `#[cfg]`-ed out of the parser, so such a template fails to compile before anything can be resolved — which is also what keeps a loader unreachable from template syntax. | `feat/mjfork-template` |
| 3 | `tmpl/ws-nbsp-before-minus-marker`, `tmpl/ws-nbsp-after-minus-marker`, `tmpl/ws-vertical-tab-before-minus-marker`, `tmpl/ws-form-feed-after-minus-marker`, `tmpl/ws-nbsp-indent-lstrip`, `tmpl/ws-nbsp-raw-minus` | Whitespace control | Trim markers, `lstrip_blocks` and the raw-block scanner trim the four ASCII characters `" \t\n\r"`. | They trim Unicode whitespace, matching Rust's `char::is_whitespace` / `trim_end` / `trim_start`; the raw-tag scanner trims Rust's `is_ascii_whitespace` set, which includes the form feed. | Prompt bytes: a non-breaking space or vertical tab beside a `-` marker survived here and did not survive in BAML, so identical templates rendered different bytes. | `feat/mjfork-template` |
| 4 | `tmpl/loop-changed`, `tmpl/loop-changed-multi-arg`, `tmpl/loop-changed-nested-loops` | `loop.changed()` | A fresh loop object per iteration, and `changed()` compares two sequence values — between them, a change reported on every call. | One loop object per loop statement, advanced in place, and `changed()` compares the argument tuples element by element: the same state and comparison Rust keeps in `vm/loop_object.rs`. | `loop.changed()` is loop metadata BAML exposes; "always changed" is wrong output, not a wording difference. | `feat/mjfork-template` |
| 5 | `tmpl/loop-depth-nested`, `tmpl/loop-depth-recursive-inner-plain`, `tmpl/loop-depth-in-macro-from-recursive`, `tmpl/loop-recursive` | `loop.depth` / `loop.depth0` | Reports the evaluator's own recursion counter, so any nesting — a loop in a loop, or in an if — inflates it. | Reports Rust's loop depth: 0 unless the enclosing loop was declared `recursive`, in which case one more than that loop's (`vm/mod.rs push_loop`); a macro body starts over at 0, because Rust evaluates it in a fresh state. | A nested loop rendered `2`/`1` where BAML renders `1`/`0`. | `feat/mjfork-template` |
| 6 | `tmpl/for-unpack-arity-mismatch`, `tmpl/set-unpack-arity-mismatch`, `tmpl/set-unpack-non-iterable`, `tmpl/set-dotted-on-plain-map`, `tmpl/set-attr-on-undefined` | Assignment targets | Unpacking outside a `for` pads missing targets with undefined and ignores extra values; a dotted assignment onto a non-namespace is silently dropped; unpacking failures are generic invalid operations. | Every assignment target reports failure: wrong arity and non-iterables raise the new `ErrCannotUnpack` kind with Rust's wording, and a dotted assignment onto anything but a namespace is an invalid operation. | Rust compiles all three forms through `UnpackList`/`SetAttr` and errors (`vm/mod.rs`). Silently dropping `{% set m.a = 2 %}` produced a *different prompt*, not a different error. | `feat/mjfork-template` |
| 7 | `tmpl/caller-without-call-block`, `tmpl/err-call-without-macro`, `tmpl/call-block-macro-without-caller`, `tmpl/macro-explicit-caller-kwarg`, `tmpl/call-non-callable-string`, `tmpl/call-undefined-binding` | Macro and call-block dispatch | `caller` is bound only when a call block supplies one; a call block on an unknown name is "call block requires a macro"; a macro that ignores `caller()` rejects a call block as an invalid operation; an explicit `caller=` keyword is "too many keyword arguments"; every failed call is `unknown callable`. | `caller` is bound — possibly undefined — whenever the macro references it; a call block resolves its callee by the same rules as a call, so an unbound name is unknown and a bound non-callable is not callable; a macro that never references `caller()` rejects the keyword argument as unknown; an explicit `caller=` is accepted as the caller. | Rust compiles a call block into an ordinary call carrying a `caller` keyword argument (`vm/macro_object.rs`), so these are all consequences of one model the port had approximated. | `feat/mjfork-template` |
| 8 | `tmpl/loop-unknown-method`, `tmpl/negative-self-block-call` | Method calls | A `receiver.name(...)` call that resolves to nothing reports `unknown function`, or an undefined-value error when the receiver is undefined. | A method call resolves against its receiver first: a missing method is the new `ErrUnknownMethod` kind, an attribute that exists but is not callable is an invalid operation, and only an unbound *name* is an unknown function. | Rust separates the three (`vm/mod.rs CallMethod`, `value/mod.rs call_method`), and BAML's pycompat callback keys off exactly the unknown-method kind. | `feat/mjfork-template` |
| 9 | `tmpl/loop-cycle-no-args` | `loop.cycle()` with no arguments | Guards the empty argument list and returns undefined, so the template renders successfully as an empty string. | No guard: the remainder is computed first, exactly as the engine computes it, so the call faults. In Go that is a recoverable runtime panic from the same expression. | The engine computes `idx % args.len()` before inspecting the argument list (`vm/loop_object.rs call_method`) and panics with a remainder-by-zero. Rendering a successful empty string here would emit a prompt BAML cannot produce — a silent byte-level divergence in the middle of otherwise byte-exact output — which is worse than reproducing the fault. This is the one input on which this engine panics; `engine_contract_test.go` pins both halves of that contract. | `feat/mjfork-template` |

### Slice 3 — the numeric core

One root cause. The port performed integer arithmetic in `float64` and cast back
with `int64(f1 op f2)` (upstream `value/ops.go:36-131`), which loses precision
above 2^53 and is implementation-defined out of range — Go's `int64(float64)`
saturates on arm64 and wraps on amd64, so the same template rendered different
bytes on different machines. The engine does all integer arithmetic in checked
`i128` after `ops::coerce`. `value/numeric.go` reproduces that model exactly, in
pure Go.

| 10 | `arith/int-add-above-2pow53`, `num/add-2pow53-plus-one`, `num/sub-2pow53-adjacent`, `num/add-2pow53-chain`, `num/mul-i64max-squared`, `num/loop-range-sum`, `num/template-accumulate` | `+ - *` | operands go through `float64`, so integers past 2^53 collapse onto one value | checked `i128` via `ops::coerce`, narrowed back with `int_as_value` | `ops.rs:274-333`. 2^53+1 and 2^53 are one `f64`; the engine keeps them apart | `feat/mjfork-numeric` |
| 11 | `arith/int-mul-i64-edge`, `num/sub-i64max`, `num/add-i64max-widens`, `num/mul-2pow62-by-two` | i64 edge | `int64(float64)` out of range: **arm64 saturated, amd64 wrapped** — the same input rendered `9223372036854775807` and `-9223372036854775808` | exact `i128`, widening to `ValueRepr::I128` past i64 | the only architecture-dependent row in the slice-1 ledger, and the reason the differential runs on both arches | `feat/mjfork-numeric` |
| 12 | `num/add-i128max-overflow`, `num/mul-i128max-overflow`, `num/sub-i128-min-overflow`, `num/pow-overflow`, `num/neg-i128-min-overflow`, `num/floordiv-i128-min-by-neg-one`, `num/abs-i128-min` | overflow | silently produced whatever `int64(float64)` gave | an error, never a wrap or a wider answer | the engine's `checked_*` return `None` there; answering at all would be a value it cannot hold | `feat/mjfork-numeric` |
| 13 | `num/i128max-literal`, `num/i128-min`, `num/u128-max-literal`, `num/u128-pair-wrapping`, `num/u128-mixed-refuses`, `num/sub-2pow127-refuses`, `num/render-bigint` | integer payload model | a `big.Int` payload with no arithmetic support at all: any operation on an integer past i64 errored | the engine's four integer `ValueRepr` variants, tagged, including `coerce`'s wrapping `(U128, U128)` arm | the repr, not the magnitude, decides two of the engine's behaviours — see the header comment in `value/numeric.go` | `feat/mjfork-numeric` |
| 14 | `arith/rem-negative`, `num/rem-neg-denominator`, `num/rem-both-negative`, `num/rem-by-zero`, `num/rem-float`, `num/rem-float-by-zero`, `num/rem-large` | `%` | Go's truncated `int64 %`, so `-7 % 2` was -1; a zero float divisor errored | `checked_rem_euclid` for integers (never negative), truncated `math.Mod` for floats (NaN on zero) | `ops.rs:302`. The integer and float arms genuinely use different conventions | `feat/mjfork-numeric` |
| 15 | `num/floordiv-neg-denominator`, `num/floordiv-both-negative`, `num/floordiv-float-neg-denominator`, `num/floordiv-by-zero`, `num/floordiv-float-by-zero` | `//` | `math.Floor(a / b)`, so `7 // -2` was -4; a zero float divisor errored | `checked_div_euclid` for integers and `f64::div_euclid` for floats | `ops.rs:382-396`. Euclidean and floored division differ whenever the remainder is zero and the signs differ | `feat/mjfork-numeric` |
| 16 | `arith/div-by-zero`, `num/div-int-exact`, `num/div-int-whole`, `num/div-neg-by-zero`, `num/div-zero-by-zero`, `num/div-large-exact`, `num/div-bool` | `/` | errored on integer division by zero | unconditionally `f64`, so division by zero is ±inf or NaN | `ops.rs:373-380`. `div` does not go through `coerce` at all | `feat/mjfork-numeric` |
| 17 | `num/pow-negative-exponent`, `num/pow-exponent-past-u32`, `num/pow-i128-edge`, `num/pow-zero-zero`, `num/pow-neg-base`, `num/pow-neg-base-even`, `num/pow-left-assoc` | `**` | `math.Pow` then a cast back to `int64` when the result looked integral | `i128::checked_pow` with a `u32` exponent; a negative, fractional or oversized exponent is an error | `ops.rs:398-410`. The port's fallback to floating point silently answered where the engine refuses | `feat/mjfork-numeric` |
| 18 | `num/pow-float-base-2pow63`, `num/pow-promoted-composition`, `num/float-literal-rounds-to-2pow49`, `num/rem-integral-float`, `num/rem-integral-float-both`, `num/pow-int-float-integral-exponent`, `num/int-plus-integral-float` | integral floats | `Rem` and `Pow` read both operands through `AsInt`, which accepts an integral float, so `2.0 ** 63` became `int64(float64(2^63))` | a float payload puts the operation in `f64` for every operator; the PAYLOAD decides, not the value | the [#649](https://github.com/invakid404/baml-rest/pull/649) round-10/11/12 class, closed at the operator instead of by a static guard | `feat/mjfork-numeric` |
| 19 | `num/eq-2pow53-adjacent`, `num/lt-2pow53-adjacent`, `num/eq-int-float-lossy`, `num/eq-bigint-int`, `num/lt-bigint`, `num/eq-i64max-adjacent`, `num/input-int-2pow53-eq` | `== != < >` on numbers | compared as `float64`, so `9007199254740993 == 9007199254740992` was true | `ops::coerce(_, _, false)` — the LOSSLESS coercion — then `i128` or `f64` | `value/mod.rs:496-507,634-637`. Exact arithmetic is worth nothing if the comparison that reads it is not exact | `feat/mjfork-numeric` |
| 20 | `num/lt-neg-zero`, `num/lt-nan`, `num/gt-nan`, `num/eq-nan`, `num/eq-neg-zero` | float ordering | Go's `<`/`>`, which report neither for NaN and cannot separate -0.0 from 0.0 | `f64_total_cmp`, a total order | `value/mod.rs:597-604` | `feat/mjfork-numeric` |
| 21 | `num/add-bool-int`, `num/add-bool-bool`, `num/mul-bool-int`, `num/eq-bool-int`, `num/lt-bool-int`, `num/is-odd-bool`, `num/int-filter-bool`, `num/index-bool-key`, `num/range-bool` | bool as an integer | `AsInt`/`AsFloat` had no bool arm, so `true + 1` was an error | a bool converts to 0/1 wherever the engine's `TryFrom` does | `argtypes.rs:413`. The bool arm is the first line of the engine's own integer conversion | `feat/mjfork-numeric` |
| 22 | `num/neg-bool`, `num/abs-bool`, `num/is-integer-bool` | where a bool is NOT a number | `Neg` type-switched on the payload; `abs` and `is integer` asked through a conversion | `neg` and `abs` are guarded on the payload being a number, and `is integer` asks `Value::is_integer` | `ops.rs:414`, `filters.rs:531-549`, `value/mod.rs:1138`. Adding the bool arm to the conversion is what made the consumers that must NOT accept a bool worth stating explicitly | `feat/mjfork-numeric` |
| 23 | `num/int-filter-1e30`, `num/int-filter-1e40`, `num/int-filter-neg-1e40`, `num/int-filter-nan`, `num/int-filter-inf`, `num/int-filter-2pow63-float`, `num/int-filter-string-i128`, `num/int-filter-string-float`, `num/int-filter-bigint`, `num/abs-i64-min`, `num/abs-float`, `num/abs-bigint`, `num/round-bigint`, `num/float-filter-bigint` | `int`, `abs`, `round`, `float` | `int64(float64)` again, at i64 width and architecture-dependently | Rust's saturating `f64 as i128`, and payload dispatch for `abs`/`round` | `filters.rs:531-588,659-673`. The engine's `int` works at i128 width, so `1e30\|int` is exact | `feat/mjfork-numeric` |
| 24 | `num/is-even-bigint`, `num/is-even-2pow63-float`, `num/is-integer-bigint`, `num/is-integer-integral-float`, `num/input-float-2pow63-is-even` | `is odd`, `is even`, `is integer` | `AsInt` at i64 width, so an integer past i64 answered false and a 2^63 float answered from an architecture-dependent cast | `i128::try_from` for the parity tests, `Value::is_integer` for the payload test | `tests.rs:133-145,179-181`. The [#649](https://github.com/invakid404/baml-rest/pull/649) round-13 case | `feat/mjfork-numeric` |
| 25 | `container/list-slice`, `num/slice-frac-start`, `num/slice-frac-stop`, `num/slice-frac-step`, `num/slice-string-start`, `num/slice-float-start`, `num/slice-2pow63-float-start`, `num/slice-none-bound` | slice bounds | a bound that did not convert was left UNSET, which is indistinguishable from an omitted bound, so `xs[1.5:]` sliced the whole sequence | a present, non-`none` bound that does not convert is an error | `ops.rs:133-148`. The [#649](https://github.com/invakid404/baml-rest/pull/649) round-15 case | `feat/mjfork-numeric` |
| 26 | `num/range-float`, `num/range-frac`, `num/range-huge-float` | `range` arguments | `stop, _ = args[0].AsInt()` — a failed conversion silently became 0 | a bound that does not convert is an error | `functions.rs:326`. Same class as the slice bounds, at the other `AsInt` consumer the brief names | `feat/mjfork-numeric` |
| 27 | `num/render-one-third`, `num/render-int-valued-float`, `num/render-1e300`, `num/render-1e-10`, `num/render-nan`, `num/render-neg-zero`, `num/render-float-epsilon`, `num/render-float-in-list`, `num/render-inf`, `num/render-neg-inf` | float rendering | `%g` — six significant digits — so `1 / 3` rendered `0.333333`, and NaN rendered `nan` | the shortest round-tripping positional decimal with a forced `.0`, and `NaN` | `value/mod.rs:697-709`. Exact arithmetic that renders to different bytes is not parity; the fork's own lexer already normalized float literals this way | `feat/mjfork-numeric` |
| 28 | `num/mul-string-repeat-float`, `num/mul-string-repeat-negative`, `num/index-float-key`, `num/index-frac-key`, `num/index-2pow63-float-key` | the remaining `AsInt` consumers | inherited the old conversion | inherited the corrected one; the consumers themselves are unchanged | fixing `Value.AsInt` at the source is what closes the class, rather than one guard per consumer | `feat/mjfork-numeric` |
| 29 | `num/eq-i64max-vs-2pow63-float`, `num/eq-u64max-vs-2pow64-float`, `num/eq-i64max-self`, `num/lt-i64max-vs-2pow63-float` | integer-literal repr (`ValueRepr::U64`) | every integer that fits `int64` was one payload, so a LITERAL and a host-supplied `i64` were indistinguishable | a literal is a `u64Value` (ValueRepr::U64); `FromInt` stays I64 and the new `FromUint64` is U64 | `lexer.rs:481-491` lexes a literal with `u64::from_str_radix`, and `as_f64`'s lossless check casts the f64 back through the operand's OWN type. `i64::MAX`'s f64 image is 2^63, which round-trips through i64 and not through u64; `u64::MAX`'s is 2^64, which round-trips through u64. The two rows answer in opposite directions, so the repr is not a boundary refusal | `feat/mjfork-numeric` |
| 30 | `num/lt-int-float-lossy`, `num/gt-float-int-lossy`, `num/lt-u128-vs-int`, `num/sort-uncomparable`, `num/eq-u128-vs-int`, `num/eq-int-float-lossy` | ordering two numbers that do not coerce losslessly | returned "not comparable", which the VM turned into an invalid-operation error | panics with `value.UncomparableNumbers`, which is what the engine does | `Ord::cmp` handles a failed coercion by assuming both operands are objects and unwrapping `self.as_object()` (`value/mod.rs:638-641`); for two numbers that unwrap is on a `None`. Returning an error instead was a different observable outcome on an input BAML can reach. Only ORDERING aborts — `PartialEq` handles the same failure safely and answers false, and so does the fork | `feat/mjfork-numeric` |
| 31 | `num/u128-mixed-refuses`, `num/sub-2pow127-refuses`, `num/neg-bool`, `num/mul-string-repeat-negative` | Numeric error wording | the port's own phrasing (`cannot add number and number`), and a string repetition with a bad count fell through to the numeric arm and reported the operand KINDS as unsupported | `ops::impossible_op`'s wording, `Error::from(ErrorKind::InvalidOperation)`'s empty detail, and the engine's own `strings can only be multiplied with integers` | `ops.rs:240-250,304-315`, `value/mod.rs`. The template slice's `oracle/messages_test.go` compares the text of every error both engines produce, because BAML surfaces it to a caller when a prompt fails to render; these four rows are what the numeric lane owed that surface. The empty-detail case needed a way to say "no detail" that is not "an empty detail": `NewErrorWithoutDetail` in `internal/errors`, reached through a sentinel the `value` package raises and `wrapEvalError` recognises. An earlier revision inferred it from an empty `Message`, which was WRONG — an empty detail is still a detail, and `GetTemplate("")` passes the template name straight through, so that inference silently changed `template not found: ` to `template not found`. `numeric_contract_test.go` pins both forms | `feat/mjfork-numeric` |

### Error message wording

The template sweep also aligns the *text* of engine errors with BAML's. That is
not a behavioural delta — the differential compares categories, not prose — but
it is checked rather than assumed: `oracle/messages_test.go` compares the
message both engines produce for every corpus row that fails on both sides, and
fails on drift in either direction. Three rows are declared exceptions there,
each owned by a later slice.

## Known divergences not yet patched

The differential records 10 declared divergences from BAML's engine, all classed
`engine`, and **none of them belongs to the template class or the numeric
class**: every row in those two lanes agrees with the engine. They are listed
with their evidence in `oracle/divergences.json`.

| Slice | Corpus IDs |
| --- | --- |
| 2 — generic `value_cmp` API | `valuecmp/object-eq-string`, `valuecmp/string-eq-object`, `valuecmp/string-in-object-list`, `valuecmp/object-eq-object` |
| 4 — coercion and containers | `cmp/int-in-string`, `container/map-render-insertion-order`, `container/map-loop-insertion-order` |
| 5 — builtins and pycompat | `str/lower-dotted-capital-i`, `str/upper-sharp-s`, `err/go-only-urlencode-filter` |

The template slice had two entries in this table and both are closed:
`err/syntax-incomplete-if` by patch #1 and `tmpl/loop-cycle-no-args` by patch #9.
The numeric slice had four — `arith/int-add-above-2pow53`,
`arith/int-mul-i64-edge`, `arith/rem-negative` and `arith/div-by-zero` — and all
four are closed by patches #10 to #16.

### The numeric class carries no exception, and cannot

Slice 3's exit gate is byte-exactness with no decline, so a numeric row may not
be carried as a declared divergence at all. Two mechanisms enforce that from
opposite sides, so a mismatch has nowhere to go:

- `TestDifferential` FAILS on a numeric row whose verdict is
  `known-divergence`, where a row from another lane is merely reported.
  `Report.Failures` does the same, so `go run ./cmd/oracle report` exits
  non-zero too.
- `TestNumericClassIsByteExact` fails if `divergences.json` names a numeric row
  at all, and separately requires every numeric row's verdict to be an exact
  match.

An undeclared numeric mismatch fails the first; declaring it fails the second.
The numeric class is the whole `numeric` lane plus every row with surface
`arithmetic`, so a numeric regression cannot escape into the seed lane.

### The engine's two faults

Both slices landed on the same conclusion independently, and it is worth stating
once: where the engine faults, the fork faults, because rendering a successful
result there would emit a prompt BAML cannot produce.

Patch #9 makes `{{ loop.cycle() }}` panic. Patch #30 makes ORDERING two numbers
that do not coerce losslessly panic — the engine's `Ord` handles a failed
coercion by unwrapping `self.as_object()`, which for two numbers is a `None`.
Equality over the same operands is unaffected in both engines.

These are the only two inputs on which this engine faults. Both are recoverable,
both are pinned from both sides — `engine_contract_test.go` and
`value/numeric_test.go` in the root module, plus differential rows that compare
panic against panic rather than declaring an exception — and the numeric one
carries an exported `value.UncomparableNumbers` so a caller that recovers can
identify it. Callers rendering untrusted templates should recover, the same
precaution they would take around a Rust engine that can abort.

**The panic DIAGNOSTIC is outside the byte-exact contract, deliberately and
explicitly.** Aborting evaluation is the whole semantic outcome of a panic row,
and that is what the differential compares; the accompanying text is the host
language runtime's message for the fault, not either engine's. Patch #9 already
ships that asymmetry — Rust says `attempt to calculate the remainder with a
divisor of zero`, Go says `runtime error: integer divide by zero` — and patch
#30 is the same situation one step further: Rust's text names `Option::unwrap()`,
an API this fork does not have, so emitting it verbatim would be a Go library
reporting a Rust diagnostic, no more faithful and less useful to a caller that
recovers.

Out of contract does not mean unchecked. `oracle/panics_test.go` PINS both
diagnostics for every panic row, from both sides, alongside the status parity
that IS the contract. A row that starts or stops faulting fails; a change to the
recorded engine diagnostic fails; a change to the fork's fails; and a new panic
row anywhere in the corpus fails until it is pinned deliberately.
