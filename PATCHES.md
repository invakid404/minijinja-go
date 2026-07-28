# Intentional semantic deltas

Every deliberate behavioural difference between this fork and its upstream
baseline is recorded here, keyed by the differential corpus ID that pins it.

The baseline is `v2.16.0-baml.2` — upstream `mitsuhiko/minijinja@b9afca`
(`minijinja-go/v2.16.0`) with no semantic change at all, proven mechanically by
[`scripts/verify-seed.sh`](scripts/verify-seed.sh). Everything below is a delta
over that baseline, introduced by the template sweep (slice 6 of the scope's
plan) so that statement and control rendering match BAML's engine
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

### Error message wording

The template sweep also aligns the *text* of engine errors with BAML's. That is
not a behavioural delta — the differential compares categories, not prose — but
it is checked rather than assumed: `oracle/messages_test.go` compares the
message both engines produce for every corpus row that fails on both sides, and
fails on drift in either direction. Three rows are declared exceptions there,
each owned by a later slice.

## Known divergences not yet patched

The differential records 14 declared divergences from BAML's engine, all classed
`engine`, and **none of them belongs to the template class**: every row in the
template lane agrees with the engine. They are listed with their evidence in
`oracle/divergences.json`.

| Slice | Corpus IDs |
| --- | --- |
| 2 — generic `value_cmp` API | `valuecmp/object-eq-string`, `valuecmp/string-eq-object`, `valuecmp/string-in-object-list`, `valuecmp/object-eq-object` |
| 3 — numeric core | `arith/int-add-above-2pow53`, `arith/int-mul-i64-edge`, `arith/rem-negative`, `arith/div-by-zero` |
| 4 — coercion and containers | `cmp/int-in-string`, `container/map-render-insertion-order`, `container/map-loop-insertion-order` |
| 5 — builtins and pycompat | `str/lower-dotted-capital-i`, `str/upper-sharp-s`, `err/go-only-urlencode-filter` |

The template slice had two entries in this table and both are closed:
`err/syntax-incomplete-if` by patch #1 and `tmpl/loop-cycle-no-args` by patch #9.

### The engine's one fault

Patch #9 makes `{{ loop.cycle() }}` panic, because that is what the engine it
mirrors does. Everything else in the corpus renders or returns an error; this is
the only template input that faults, it is recoverable, and it is pinned from
both sides — `engine_contract_test.go` in the root module, and the differential
row, which compares panic against panic rather than declaring an exception.
Callers rendering untrusted templates should recover, the same precaution they
would take around a Rust engine that can abort.
