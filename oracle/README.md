# The differential oracle

One corpus, two engines, one comparison.

```
oracle/corpus/*.json ──┬──> oracle/harness (Rust)  ─> boundaryml/minijinja@8cfc770  ─┐
                       │                                                            ├─> compare ─> oracle/divergences.json
                       └──> oracle (Go)            ─> this fork                     ─┘
```

The corpus is split by lane — `seed.json`, `template.json`, `numeric.json`,
`coercion.json`, `builtins.json`, `argcontract.json`, `reviewfixes.json` — and
each file is recorded independently as `recorded/rust-8cfc770-<lane>.json`. Adding rows to one lane therefore never
invalidates another lane's recording. Row ids are unique across the whole set,
because the ledger and `PATCHES.md` are keyed by them.

The Rust harness links **the exact engine revision BAML v0.223 builds against**
(`boundaryml/minijinja`, branch `value-cmp`, rev `8cfc770a5dffeda2de5b910d2b9f870d7edeff7c`)
with **BAML's exact cargo feature set** (copied from `BoundaryML/baml@85247f45`
`engine/Cargo.toml:99-115`, including `default-features = false` and the
deliberate omission of `multi_template` and `loader`). It reads a corpus row and
prints the exact rendered bytes, the normalized boolean, or the error category.
The Go side runs the same row through this fork and compares.

## What this oracle is, and what it is not

It is a **diagnostic microscope on the engine**. It answers "does our Go engine
behave like BAML's Rust engine on this input?"

It is **not** the authoritative whole-stack oracle. Per the scope, that is stock
BAML v0.223 driven through CFFI, because only that executes BAML's real
environment (`get_env`, pycompat, `regex_match`/`sum`), the two BAML value
lowering paths, aliases, enum globals and macro injection. Reconstructing those
by hand here is exactly where a plausible-looking harness drifts. When the CFFI
lane lands, a three-way result becomes diagnostic:

| engine agrees | CFFI agrees | conclusion |
| --- | --- | --- |
| yes | no | BAML profile / value-adapter layer (`class: profile`) |
| no | no | this fork's engine (`class: engine`) |
| all three differ | | the harness is incomplete or the fixture is wrong (`class: harness-incomplete`) |

That is why corpus rows carry a `profile` field. Two exist today:

| profile | environment |
| --- | --- |
| `stock` | stock engine defaults |
| `pycompat` | stock defaults plus the Python-compatible unknown-method callback — `minijinja-contrib::pycompat` on the Rust side, this fork's `pycompat` package on the Go side. It is a *generic* engine capability driven by an installable module, which is why it is not the BAML profile: no `regex_match`, no `sum`, no none-formatter. |

The BAML v0.223 profile arrives as a third value without a schema change.

## Running it

```bash
cd oracle

# Full differential against a live Rust engine build (needs cargo).
go run ./cmd/oracle report

# Same, replaying the committed recording instead (no Rust toolchain needed).
MJ_ORACLE_RECORDED_ONLY=1 go run ./cmd/oracle report

# As a test. Fails on any undeclared or changed divergence.
go test ./...

# Refresh the recording after changing the corpus or the harness.
./record.sh

# Regenerate internal/unicodecase from the Rust toolchain's Unicode tables and
# the pinned unicase crate's fold table. MJ_RUSTC_VERSION is read by option_env!,
# so it has to be set when the generator is COMPILED, not when it is run.
(cd harness && MJ_RUSTC_VERSION="$(rustc --version)" cargo build --release --bin mj-casegen)
./harness/target/release/mj-casegen > ../internal/unicodecase/testdata/rust-unicode.json
go run ./cmd/gentables
```

Each `recorded/rust-8cfc770-<lane>.json` is a committed run of the harness. It
carries the SHA-256 of the corpus bytes it was produced from, so it cannot be
replayed against a corpus it does not correspond to — a stale recording is a
hard error, never a quietly weaker test.

## The comparison contract

Both sides produce the same `Outcome` shape, and only part of it is compared:

| field | compared | why |
| --- | --- | --- |
| `status` | yes | ok / error / panic / unsupported are different outcomes |
| `render` | yes, byte for byte | prompt parity is a bytes question |
| `boolean` | yes, when the row expects one | BAML decides a constraint by parsing `true`/`false` |
| `category` | yes | canonical error vocabulary shared by both sides |
| `kind`, `message` | **no** | two engines are not expected to word an error identically; pinning text would drown real divergences in noise |

Message text is not ignored, though: `messages_test.go` compares the text of
every error both engines produce, with a short explicit exception list. It is a
separate test on purpose — a wording change is a diff worth seeing, but not one
that should be able to mask a behavioural divergence in the table above.

A panic is an outcome, not a crashed run. So is "the harness cannot model this
row" (`unsupported`), which is what makes `harness-incomplete` distinguishable
from a real engine difference instead of being silently scored as one.

### Panic diagnostics

A panic reduces to just `panic` in the compared signature. Aborting evaluation
is the whole semantic outcome of a panic row; the accompanying text is the host
language runtime's message for the fault, not either engine's — Rust says
`attempt to calculate the remainder with a divisor of zero` where Go says
`runtime error: integer divide by zero`, for the same fault. Reproducing the
other language's wording would be less useful to a caller that recovers and no
more faithful, since the fault itself is what is being reproduced.

Out of contract is not unchecked. `panics_test.go` PINS both diagnostics for
every panic row, from both sides, alongside the status parity that *is* the
contract. A row that starts or stops faulting fails, a change to either
diagnostic fails, and a new panic row anywhere in the corpus fails until it is
pinned deliberately. So a claim of byte-exactness means byte-exact on the
compared signature, with the one field outside it pinned rather than ignored.

## The ledger

`divergences.json` is the permanent record of known differences. The test fails
on:

- a mismatch that is **not** in the ledger — the point of the whole exercise;
- a ledger entry whose recorded signatures no longer match — a divergence that
  changed shape must be re-examined, not absorbed;
- a ledger entry that no longer diverges — a fix must remove its entry
  deliberately;
- a ledger entry with no corpus row — the regression it names is not being run.

Each entry carries a `class` (`engine`, `profile`, `host`,
`harness-incomplete`) and the slice from the scope's plan where it is expected
to be closed.

## Corpora

**`seed.json`** — 25 rows spanning arithmetic, comparison, string, container,
`value_cmp` object, environment and control surfaces, in both expression and
full-template form. Deliberately small: enough to prove the plumbing end to end
and to pin one known divergence per class.

**`template.json`** — 192 rows: the full statement and control surface. `if`/
`elif`/`else`, `for` with loop metadata (index, revindex, first/last/length,
depth, adjacent items, `cycle`, `changed`, recursion), `set` in all its forms,
`with`, `filter`, `autoescape`, `do`, comments, `raw`, macros and `call` blocks,
the error surface, and the negative controls for the statements BAML's engine
does not have. Whitespace is a family of its own: trim markers, `trim_blocks`,
`lstrip_blocks`, CR/LF, leading spaces, trailing newlines and non-ASCII
whitespace, run under the engine profiles below.

**`numeric.json`** — 146 rows: the numeric core. The 2^53 boundary and the i64,
i128 and u128 edges through every operator and through the value model;
negatives and zero divisors through `//`, `%`, `/` and `**`, where the engine's
Euclidean and checked semantics differ from Go's; overflow, which is an error in
the engine and never a wrap; integral floats, where the engine's PAYLOAD type
decides and the source spelling does not — including the cases
[#649](https://github.com/invakid404/baml-rest/pull/649) reverse-engineered
(`2.0 ** 63`, `(2.0 ** 52) * (2.0 ** 11)`, `562949953421312.000000000000001`);
the integer-literal repr in both directions, since a literal is lexed as a `u64`
and the lossless coercion casts a float back through the operand's own type, so
`i64::MAX` and `u64::MAX` answer OPPOSITELY against their own f64 images;
deterministic float-to-int conversion at every consumer the engine has —
operators, indexing, all three slice bounds, `range`, and the `int`, `abs`,
`round` and `float` filters; the five inputs on which the engine's ORDERING
panics, reached through operators and through `sort`, beside the equality rows
over the same operands that answer normally; and float rendering, since exact
arithmetic that renders to different bytes is not parity.

**A corpus row must be architecture-deterministic.** The NaN ordering rows take
their NaN from `'nan'|float` rather than from `0.0 / 0.0`, because the sign of a
hardware-produced NaN is not portable — `0.0 / 0.0` is a positive NaN on arm64
and a negative one on amd64 — and `f64_total_cmp` orders by the bit pattern.
Running the *Rust harness* on both architectures is what surfaced that: it
disagreed with itself on exactly those two rows. A defect in the fixture, not in
either engine.

**`coercion.json`** — 256 rows for the coercion, comparison, container and VM
class: equality, ordering and containment over a cross-product of typed operands
**in both operand orders**; truthiness, `and`/`or`/`not`, ternaries, `~` and `is`
tests; the generic `value_cmp` hook from either side; ordered-mapping iteration,
display, `items`, `reverse` and JSON; subscript and slice edges (bools, negative
steps, bounds past either end, inverted ranges, non-integer bounds, the i64
float boundary); `range`; and the display form of every container shape. The rows
the fork agrees on are as load-bearing as the red ones: they are what makes a
regression in this class fail.

**`builtins.json`** — 395 rows: every filter, test and function BAML's engine
build enables, each with valid input, bad input and the exact error; the whole
`minijinja-contrib::pycompat` dispatch table, run on the `pycompat` profile;
printf and `str.format` specs; Unicode casing, whitespace and digit edges; JSON
and float formatting; and the five Go-only names (`urlencode`, `containing`,
`cycler`, `joiner`, `lipsum`) that must be *rejected* with the engine's own
unknown-filter/test/function error.

**`argcontract.json`** — 440 rows: the engine's `from_args` contract itself.
Arity and keyword handling for all 94 registered names, the invalid-value
boundary (`ValueRepr::Invalid` and the engine's four validation points), call
dispatch (`unknown_function` for an unresolved name versus `invalid_operation`
for a resolved non-callable), bare-call evaluation order, and macro lifetime
and `{% call %}` semantics.

### Engine profiles

`trim_blocks`, `lstrip_blocks` and `keep_trailing_newline` cannot be reached
from template source — they are environment settings — so a row can name one of
six profiles, and both sides configure the environment identically:
`stock`, `trim_blocks`, `lstrip_blocks`, `trim_lstrip` (the pair BAML's own
environment sets), `keep_trailing_newline`, and `pycompat`, which installs the
Python-compatible unknown-method module BAML installs on its own environment
(`minijinja-contrib::pycompat` on the Rust side, this fork's `pycompat` package
on the Go side). Every profile is *engine configuration only*; BAML's
environment (globals, `regex_match`/`sum`, the none-formatter, prompt lowering)
is not here and arrives as its own profile in a later slice.

**`reviewfixes.json`** — 228 rows: the cases two rounds of cold review found by
probing the pinned engine directly rather than by reading the corpus. `range`
cardinality at the i64 boundary; integer ArgTypes at their declared widths,
including `usize` at its real 64-bit one; integers past i64 through the tests
and the formatters; the string tests' typing; composite sort and select paths;
the pycompat view objects' kind, indexing and truth; `debug()`'s exact bytes;
whether a macro accepts the synthetic `caller` keyword, decided the way the
compiler decides it; `dict()`'s key spellings; the engine's one case-insensitive
comparator across `sort`, `groupby` and `dictsort`; and `groupby`'s two
observable kinds. A row here is a repro that was RED against the engine before
it was a row.

### Where the corpus stands

1682 rows: 1670 agree, 12 diverge and every one of the 12 is declared. None is
in the template, numeric, coercion or argument-contract lane, whose rows all
agree with the engine. Ten of the twelve are deliberate and permanent rather
than pending:

- `test/divisibleby-zero` — the engine PANICS on a zero divisor and the fork
  refuses with an error instead.
- `review/usize-batch-u64-upper`, `review/usize-slice-u64-upper`,
  `review/usize-indent-u64-upper`, `review/usize-tojson-u64-upper`,
  `review/usize-batch-u64-max`, `review/usize-batch-float-u64-upper`,
  `review/usize-batch-i64-max` — the same disposition one layer up. A `usize`
  argument that sizes an allocation converts on both sides; the engine then
  reserves that much memory and aborts with a capacity overflow, and the fork
  refuses. That the CONVERSION agrees is proven separately and greenly by
  `review/usize-*-too-many`, where the value converts and the call fails on its
  arity instead.
- `review/pycompat-count-empty-needle` — `"abc".count("")` does not terminate in
  the reference module; the outcome is recorded as a timeout rather than the row
  being left out.
- `fn/debug-state-dump` — `debug()` with no arguments prints the host language's
  debug rendering of the environment, Rust type paths included.

Two are pending, and both belong to the error surface:

- `syntax/bad-escape-capital-u`, `syntax/bad-escape-rust-unicode` — the lexer's
  string escapes, owned by slice 6. Both sit in the builtins lane because that
  is where they were found, not because they are builtins.

`container/dict-function-kwargs-order` used to be here and is gone: the callable
signature now carries an ordered keyword mapping (`value.Callable`,
`value.MethodCallable`, `value.CallableObject`, `filters.FilterFunc`,
`FunctionFunc` all take a `*value.OrderedMap`), so `dict(b=1, a=2)` keeps its
order and an unknown-keyword error names the first one written. See PATCHES.md
#69 and #81.

Error TEXT is checked as well as error category: `messages_test.go` compares the
message both engines produce for every row that fails on both sides, and carries
two declared wording exceptions, one owned by slice 4's value model and one by
the error surface.

`arith/int-mul-i64-edge` used to be **architecture-dependent**: the fork rendered
`9223372036854775807` on darwin/arm64 and `-9223372036854775808` on linux/amd64
for the same input, because Go leaves an out-of-range `int64(float64)`
implementation-defined. The first cross-platform CI run found it, because the
ledger refuses a divergence that changed shape. That is why the differential
runs on both architectures; slice 3 closed it, and the row is now the same on
both.

### A landed lane may not carry a declared divergence

A declared divergence is a record for the slice that closes it — but once a
slice has landed, declaring a mismatch in its lane must not be a way to make the
differential green, or the ledger becomes a place to hide a decline. Slice 3's
exit gate is byte-exactness with no decline, so `Report.Failures` and
`TestDifferential` FAIL on a known divergence in the numeric class, and
`TestNumericClassIsByteExact` fails if the ledger names a numeric row at all. An
undeclared numeric mismatch fails the first; declaring it fails the second. The
class is the whole `numeric` lane plus every row with surface `arithmetic`, so a
numeric regression cannot escape into the seed lane. A later slice adds its own
lane the same way as it lands.

**The builtins lane does not have that gate, and the omission is deliberate.**
Its three lanes (`builtins`, `argcontract`, `reviewfixes`) hold ten ledger
entries and none of them is a decline being hidden:

| Row | Why it is not a numeric-style failure |
| --- | --- |
| `test/divisibleby-zero` | The engine PANICS; the fork errors. A gate demanding byte-exactness here would demand reproducing a panic. |
| `review/usize-*` (7 rows) | The same: the engine reserves an unallocatable amount of memory and aborts. A gate here would demand that a Go library abort, or exhaust its memory, on template input. The conversion those rows are really about IS gated, by the green `review/usize-*-too-many` rows beside them. |
| `review/pycompat-count-empty-needle` | The reference module does not terminate. A gate would demand reproducing a non-terminating loop. |
| `fn/debug-state-dump` | The engine's bytes are Rust type paths. A gate would demand hard-coding them into a Go engine. |
| `syntax/bad-escape-capital-u`, `syntax/bad-escape-rust-unicode` | Lexer rows that merely live in this lane; the error surface owns them. |

Every one of those is a SAFETY or host-language decision, and every one of them
is a row that runs on both sides on every run — not an omission. So the builtins
gate is stated rather than automated: **every row that is about a builtin agrees
byte for byte, including its error text.** A reviewer checking that claim should
read the ledger, not a passing test — which is exactly why the entries above are
enumerated here.

## Rust is test-only

The harness is a separate Cargo project and the runner is a separate Go module
(`github.com/invakid404/minijinja-go/oracle`) with a local `replace`. Neither is
reachable from the published engine module, `go build ./...` at the repo root
never sees them, and the two talk over stdout JSON — no CGO, no linkage. The
fork itself stays pure Go, which CI asserts separately.
