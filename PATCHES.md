# Intentional semantic deltas

Every deliberate behavioural difference between this fork and its upstream
baseline is recorded here, keyed by the differential corpus ID that pins it.

The baseline is `v2.16.0-baml.2` — upstream `mitsuhiko/minijinja@b9afca`
(`minijinja-go/v2.16.0`) with no semantic change at all, proven mechanically by
[`scripts/verify-seed.sh`](scripts/verify-seed.sh). Everything below is a delta
over that baseline, introduced by the template sweep (slice 6 of the scope's
plan), the numeric core (slice 3) and the coercion, container and VM operations
(slice 4), so that the fork matches BAML's engine
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

### Slice 4 — coercion, containers and the VM

Comparison, containment, truthiness and the container operations, made
byte-exact with the target engine, plus the generic `value_cmp` dispatch hook
that is BoundaryML's sole engine delta over upstream. The numeric core (above)
supplies the coercion these entries compare THROUGH; what they own is the shape
of the operation around it — which arms run in which order, what falls through
to the hook, and how containers enumerate.

| 32 | `valuecmp/object-eq-string`, `valuecmp/string-eq-object`, `valuecmp/object-eq-object`, `valuecmp/string-in-object-list`, `valuecmp/object-in-string-list`, `valuecmp/list-eq-string-list`, `valuecmp/object-lt-string`, `valuecmp/object-gt-string`, `valuecmp/string-lt-object`, `valuecmp/string-gt-object`, `valuecmp/object-ne-string`, `valuecmp/object-eq-display`, `valuecmp/object-eq-int`, `valuecmp/object-lt-int`, `valuecmp/object-lt-object`, `valuecmp/object-eq-none`, `valuecmp/object-sort`, `valuecmp/object-is-eq-test`, `valuecmp/object-is-in-test` | Generic `value_cmp` dispatch (`value/valuecmp.go`, `value/ops.go`) | An object can only be compared with another object, through `ObjectWithCmp`, and only from `Compare`; `Equal` never reaches it. | New `value.ObjectWithValueCmp` hook. Equality asks the left operand's object, then the right's; ordering asks the left, then the right with the result negated, before kind ordering. Declining falls through to the ordinary rules. | This is BoundaryML's sole engine delta over upstream (`8cfc770`), and the engine half of the BAML enum-equality fence (#597): a host enum object compares by its canonical variant value while displaying an alias. The fork exposes it generically — no BAML types — and a corpus test object proves it. | `feat/mjfork-coercion` |
| 33 | `cmp/int-eq-float-lossy-boundary`, `cmp/float-eq-int-lossy-boundary-reverse`, `cmp/int-eq-string`, `cmp/string-eq-int`, `cmp/bool-eq-string`, `cmp/int-eq-bool-true`, `cmp/none-eq-*`, `cmp/undefined-eq-*`, `cmp/map-eq-map-diff-order`, `cmp/list-eq-list-numeric-coercion` | Comparison structure (`value/ops.go`) | `Equal` walked ad-hoc `As*` conversions: bools coerced through `float64`, every integer compared as `float64`, maps compared only when both were Go maps. | `Equal` is the engine's `PartialEq` in order: identical-repr fast paths, then `ops::coerce(_, _, false)` (entry #19 supplies it, in exact i128), then the `value_cmp` hook, then the container arms. Containers compare by kind: map/map by entries, seq-or-iterable by iteration, plain by rendering. A none/undefined pair that is not both falls THROUGH rather than answering false, so an object can still be asked. | Equality is a coercion question, not a conversion-convenience question. Entry #19 owns the coercion itself; what this entry owns is the order of the arms around it — an early `return false` for none, undefined or a failed coercion is what bypasses the hook and the container arms. | `feat/mjfork-coercion` |
| 34 | `cmp/none-lt-none`, `cmp/none-le-none`, `cmp/undefined-lt-undefined`, `cmp/map-lt-map`, `cmp/one-gt-nan`, `cmp/nan-lt-one`, `cmp/nan-cmp-nan`, `cmp/neg-zero-lt-zero`, `cmp/neg-zero-eq-zero`, `cmp/iterable-gt-list`, `cmp/list-lt-iterable`, `cmp/slice-gt-list` | Total ordering (`value/coerce.go`, `value/ops.go`) | `Compare` reported "incomparable" for same-kind pairs it had no rule for (none/none, undefined/undefined, map/map), which made the VM raise on `{{ none < none }}`. Floats were ordered with `<`, leaving NaN unordered and `-0.0` equal to `0.0`. | Ordering is total, as in Rust: payload-less kinds are equal to themselves, maps compare pairwise in iteration order, and bytes compare bytewise. `kindOrder` is the engine's full `ValueKind` declaration order, so the ranks now include iterable, plain and invalid. The float total order itself is entry #20. | Rust's `Ord for Value` never refuses, so a refusal is not a conservative choice: it turns a defined boolean into a template error. The float total order is also what `sort` uses. | `feat/mjfork-coercion` |
| 35 | `cmp/int-in-string`, `contains/int-in-string-multi`, `contains/float-in-string`, `contains/bool-in-string`, `contains/none-in-string`, `contains/list-in-string`, `contains/string-in-int-list`, `contains/bool-in-int-list`, `contains/in-none`, `contains/in-int`, `contains/none-in-map`, `contains/int-in-range`, `test/is-in-string` | Containment (`value/ops.go`, `state.go`) | A string container only matched a string needle, so `1 in '1'` was false. A non-container answered `false`. | New `Value.TryContains` ports the Rust engine's `contains`: a string container stringifies a non-string needle, a mapping tests keys, a sequence or iterable tests items by equality (so containment reaches the `value_cmp` hook), an undefined container is empty, and anything else is an `invalid_operation` error. `Value.Contains` keeps its boolean signature and answers `false` on that error. | `1 in '1'` is the case the forensic record named. The error matters as much as the coercion: silently answering `false` for `1 in 5` turns a broken predicate into a passing one. | `feat/mjfork-coercion` |
| 36 | `container/map-render-insertion-order`, `container/map-loop-insertion-order`, `container/map-render-nested`, `container/map-render-inside-list`, `container/map-for-key-order`, `container/map-values-order`, `container/map-list-order`, `container/map-first`, `container/map-tojson-order`, `container/map-tojson-nested-order`, `container/dict-literal-order`, `container/dict-literal-nested-order`, `container/dict-literal-duplicate-key`, `container/map-loop-index`, `container/map-items-first`, `container/map-dictsort`, `logic/or-empty-containers`, `concat/map` | Ordered mappings (`value/orderedmap.go`, `value/value.go`, `state.go`, `filters/filters.go`) | The map value is a Go `map[string]Value`, and every consumer sorted its keys. Insertion order could not be represented at all. | New `value.OrderedMap` representation with `FromOrderedMap`, `AsOrderedMap`, `AsMapValue` and `Value.MapKeys`. Map literals build one; every engine site that enumerates a mapping (display, debug, iteration, `items`, `list`, `first`, `dictsort`, `pprint`, JSON) goes through `MapKeys`. A value built from a Go map has no order and is still enumerated sorted, deterministically. | BAML builds its engine with `preserve_order`, so map order is prompt bytes: loop output, `items()`, display and `tojson` all observe it. `tojson` needed its own ordered encoder because `encoding/json` sorts Go map keys. | `feat/mjfork-coercion` |
| 37 | `container/map-render-numeric-string-key`, `container/map-render-numeric-string-key-inside-list`, `container/dict-literal-int-key` | Map key spelling (`value/orderedmap.go`, `value/value.go`) | A map's debug form printed a key that looked like an integer unquoted and its display form always quoted it, so the two disagreed and neither was right for both key types. | An `OrderedMap` remembers a non-string key's spelling, and display and debug are one function, as they are in Rust. `{1: 'a'}` renders `{1: "a"}` and a mapping with the string key `"1"` renders `{"1": 2}`, in every position. A provenance-less Go map keeps the old heuristic, which is what a splatted `**{8: 8}` kwargs mapping needs. | Rust keys maps by `Value`, this fork keys by string. Remembering the spelling closes the rendering half of that gap; the lookup half is still open (see below). | `feat/mjfork-coercion` |
| 38 | `truth/nan`, `logic/not-nan`, `truth/empty-range`, `truth/empty-slice`, `truth/empty-map` | Truthiness (`value/value.go`) | NaN was falsy, big integers were truthy by default, and an empty iterator was truthy. | Truthiness of a float is exactly `x != 0`, which makes NaN truthy; big integers test their sign; an iterator or ordered map with a known length of zero is falsy. | The Rust rule is a single `!= 0.0` comparison. Excluding NaN reads as a fix but flips `{{ 'y' if 0.0 / 0.0 }}`, and truthiness feeds `if`, `and`, `or`, `not` and ternaries. | `feat/mjfork-coercion` |
| 39 | `logic/and-returns-value`, `logic/and-const-falsy-left`, `logic/and-runtime-falsy-left`, `logic/and-runtime-falsy-right`, `logic/and-nested-const`, `logic/and-const-with-failing-operand` | Constant-folded `and` (`state.go`) | `and` always yielded an operand. | When both operands are constant expressions, `and` yields a plain `false` if the result is falsy, and the right operand otherwise; with any non-constant operand the runtime rule (yield the operand) still applies. `isConstExpr`/`foldConst` mirror Rust's `Expr::as_const`, including that an operand whose operation fails is not folded. | Not a simplification: the Rust engine's compiler folds a fully-literal expression with semantics that differ from its own VM instruction for `and` alone. `{{ 'a' and 0 }}` renders `false` and `{{ x and 0 }}` renders `0` in BAML's engine, and both are prompt bytes. | `feat/mjfork-coercion` |
| 40 | `slice/*` (29 rows), `test/is-sequence-slice`, `container/slice-eq-list`, `container/slice-plus-list`, `container/list-plus-list-is-sequence`, `range/slice` | Slicing (`state.go`, `value/ops.go`) | Bounds were resolved with ad-hoc clamping; a non-integer bound was silently treated as omitted; only sequences and strings could be sliced; the result was a sequence; slice errors were unclassifiable `fmt.Errorf` values. | `sliceOffsetAndLen` and `sliceStepBackwards` port the Rust engine's `get_offset_and_len` and `range_step_backwards` exactly. None and undefined slice into an empty sequence; iterables (a range, another slice) can be sliced; bytes can be sliced; a non-integer bound and a zero step are `invalid_operation` errors. The result is a lazy iterable, as in Rust, which `is sequence` observes. Sequence `+` accepts iterables so a slice can still be concatenated. | Slice arithmetic is where an off-by-one is invisible until it silently drops an element, and the corpus covers negative steps, both bounds past either end, and inverted ranges. Making the result lazy is what the kind tests see. Bound *conversion* is entry #25. | `feat/mjfork-coercion` |
| 41 | `range/step-zero` | `range()` error class (`defaults.go`) | A zero step returned an unclassifiable `fmt.Errorf`. | It returns `ErrInvalidOperation`, like the Rust engine's `invalid_operation`. | An external consumer classifies engine errors by kind; an unexported error type cannot be classified at all. | `feat/mjfork-coercion` |
| 42 | `range/empty`, `range/negative-step-empty` | Empty iterables (`value/value.go`) | `Value.Iter` returned `nil` both for "not iterable" and for "iterable but empty", so `range(0)|list|join(',')` rendered `[]` instead of nothing. | `Iter` returns a non-nil empty slice for an empty iterable and keeps `nil` for a value that cannot be iterated. | Callers use `nil` to mean "not iterable" and there is no other signal, so conflating the two makes an empty container fall through to a caller's "not a sequence" branch. | `feat/mjfork-coercion` |
| 43 | `container/map-reverse-order`, `container/map-reverse-three-keys`, `container/map-reverse-twice` | `reverse` on a mapping (`filters/filters.go`) | Reversing a mapping reversed its (sorted) keys. | Reversing a mapping yields its keys in the mapping's own order; reversing that result does reverse. | Faithful to the target engine, including its bug: a Rust map enumerates with a double-ended iterator and `Value::reverse` re-boxes that iterator without calling `.rev()`, so `m|reverse` iterates forward. The second reverse works because the intermediate value enumerates differently. Verified directly against `8cfc770` rather than inferred. | `feat/mjfork-coercion` |
| 44 | `container/index-bool-true`, `container/index-bool-false`, `container/string-index-bool`, `container/iterable-index-bool`, `container/map-index-bool`, `container/index-fractional-float`, `container/index-huge-float`, `container/index-negative-float`, `container/index-none`, `slice/bool-start`, `slice/bool-false-start`, `slice/bool-stop`, `slice/bool-step`, `slice/bool-both-bounds`, `slice/bool-start-negative-step`, `slice/float-step`, `slice/fractional-step`, `slice/string-step`, `slice/undefined-bound`, `slice/float-bound-at-i64-max`, `slice/float-bound-beyond-i64`, `slice/float-bound-at-i64-min`, `range/bool-stop`, `range/bool-false`, `range/bool-start`, `range/bool-step`, `range/bool-all-args`, `range/float-arg`, `range/fractional-arg`, `range/string-arg`, `range/none-arg`, `str/repeat-bool`, `str/repeat-bool-reversed`, `str/repeat-negative`, `str/repeat-fractional`, `str/repeat-integral-float`, `container/list-times-bool`, `container/bool-times-list`, `container/list-repeat-is-sequence` | VM integer conversion at every operand position (`value/value.go`, `state.go`, `defaults.go`, `value/ops.go`) | Every VM operand position that needs an integer went through `Value.AsInt`, which accepts only `int64` and an integral `float64`. A bool was not an integer, so `xs[true]` was undefined, `xs[true:]` was an invalid-bound error, `range(true)` was an empty range, and `'a' * true` failed. `range` also ignored a failed conversion entirely, turning `range(1.5)` and `range('2')` into empty ranges instead of errors. | Every one of those positions routes through `Value.AsInt`, which entries #21, #25, #26 and #28 made into the engine's primitive integer conversion (`primitive_int_try_from!`, `argtypes.rs:410-433`, reached through `Value::as_i64`/`as_usize`): a bool converts as 0/1, an integral float converts only when Rust's saturating cast round-trips, and everything else fails. What this entry adds is the operand positions that conversion had not reached — subscripts on a sequence, iterator, string and seq-object, and both repetition branches — plus the corpus that pins every position from both sides. The repetition branches also commit on the operand KIND, as `ops::mul` does, so a count that does not convert reports the count rather than falling through to numeric multiplication and reporting the operand kinds; and a repeated sequence is a lazy iterable, like a slice and like a concatenation. | Found by cold review. This was a success/error/value mismatch in the class this slice claims, in the most easily reached direction: a bool bound made a defined slice raise. This slice and the numeric slice reached the same conversion independently and from opposite ends — one from the VM operand positions, one from `Value.AsInt` itself — and the merge keeps the numeric one, which additionally handles the `u64` and `u128` reprs that only exist there. The float boundary is deliberately a round-trip check rather than a range check, because Go's own float-to-int conversion is undefined out of range and was observed differing between amd64 and arm64 — `slice/float-bound-at-i64-max` and `slice/float-bound-beyond-i64` pin both sides of it. | `feat/mjfork-coercion` |

### Error message wording

The template sweep also aligns the *text* of engine errors with BAML's. That is
not a behavioural delta — the differential compares categories, not prose — but
it is checked rather than assumed: `oracle/messages_test.go` compares the
message both engines produce for every corpus row that fails on both sides, and
fails on drift in either direction. Three rows are declared exceptions there,
each owned by a later slice.

## Known divergences not yet patched

The differential records 4 declared divergences from BAML's engine, all classed
`engine`, and **none of them belongs to the template, numeric, coercion or
container class**: every row in those lanes agrees with the engine. They are
listed with their evidence in `oracle/divergences.json`.

| Slice | Corpus IDs |
| --- | --- |
| 5 — builtins and pycompat | `str/lower-dotted-capital-i`, `str/upper-sharp-s`, `err/go-only-urlencode-filter`, `container/dict-function-kwargs-order` |

The template slice had two entries in this table and both are closed:
`err/syntax-incomplete-if` by patch #1 and `tmpl/loop-cycle-no-args` by patch #9.
The numeric slice had four — `arith/int-add-above-2pow53`,
`arith/int-mul-i64-edge`, `arith/rem-negative` and `arith/div-by-zero` — and all
four are closed by patches #10 to #16. Slice 4 had seven: the four generic
`value_cmp` rows and the three coercion/container rows
(`cmp/int-in-string`, `container/map-render-insertion-order`,
`container/map-loop-insertion-order`), all closed by patches #32 to #44. It
opened one, `container/dict-function-kwargs-order`, which is slice 5's because
the order is lost in the keyword-argument plumbing rather than in the mapping —
see the named gap below.

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

### Named gap: keyword-argument order

`dict(b=1, a=2)` renders `{"a": 2, "b": 1}` here and `{"b": 1, "a": 2}` in the
target (`container/dict-function-kwargs-order`). Entry #36 gave every other
mapping surface an order, but keyword arguments reach a function as a Go
`map[string]Value` (`value.Callable`, `filters.Filter`), so the order is gone
before the function is called. Closing it means the callable signature carrying
an ordered mapping, which is a change to the public object protocol and belongs
with the slice that reworks that surface.

### Named gap: mappings are keyed by string, not by value

Rust keys its maps by `Value`, so `{1: 'a'}` and `{'1': 'a'}` are different
mappings. This fork keys by string. Patch #37 closes the *rendering* half of that
gap by remembering a key's spelling, and `contains/int-key-in-numeric-string-key-map`
and `contains/none-in-map` show that containment already answers correctly for
string-keyed mappings, which is what BAML produces (`BamlValue` maps and class
fields are string-keyed, `baml_value.rs:20-56`).

What remains open is *lookup* by a non-string key: `{1: 'a'}[1]` is undefined
here and `'a'` in Rust, and iterating such a mapping yields the key as a string
rather than as a number. There is no corpus row asserting the fork's behaviour
because there is no BAML surface that reaches it; a row would pin a difference
nobody consumes.

Closing it properly is a value-model change, not a patch: `OrderedMap` would key
its index by a canonical `(kind, text)` pair rather than by the key text, expose
`KeyValues() []Value` alongside `MapKeys()`, and `Value.GetItem`, containment,
equality, ordering and the map-consuming filters would take key values instead of
key strings. `AsMap() map[string]Value` would remain as a lossy convenience.
That touches every map site in the engine and the public object protocol, so it
belongs in a slice of its own rather than being smuggled into this one.
