# The differential oracle

One corpus, two engines, one comparison.

```
oracle/corpus/*.json ──┬──> oracle/harness (Rust)  ─> boundaryml/minijinja@8cfc770  ─┐
                       │                                                            ├─> compare ─> oracle/divergences.json
                       └──> oracle (Go)            ─> this fork                     ─┘
```

The corpus is split by lane — `seed.json`, `template.json`, `numeric.json` — and each file is
recorded independently as `recorded/rust-8cfc770-<lane>.json`. Adding rows to
one lane therefore never invalidates another lane's recording. Row ids are
unique across the whole set, because the ledger and `PATCHES.md` are keyed by
them.

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

That is why corpus rows carry a `profile` field even though `stock` is the only
value today: the BAML profile arrives as a second profile on both sides without
a schema change.

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

### Engine profiles

`trim_blocks`, `lstrip_blocks` and `keep_trailing_newline` cannot be reached
from template source — they are environment settings — so a row can name one of
five profiles, and both sides configure the environment identically:
`stock`, `trim_blocks`, `lstrip_blocks`, `trim_lstrip` (the pair BAML's own
environment sets) and `keep_trailing_newline`. Every profile is *engine
configuration only*; BAML's environment (globals, filters, pycompat) is not
here and arrives as its own profile in a later slice.

### Where the corpus stands

363 rows: 353 agree, 10 diverge and every one of the 10 is declared — none of
them in the template lane or the numeric lane, whose rows all agree with the
engine. The `value_cmp` rows are the generic form of BAML's #597 enum fence: a
host object with a canonical comparison identity (`RED`) and a different display
(`Red`). Its display row agrees on both engines, which is what isolates the
remaining four rows to comparison dispatch rather than to the fixture.

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

## Rust is test-only

The harness is a separate Cargo project and the runner is a separate Go module
(`github.com/invakid404/minijinja-go/oracle`) with a local `replace`. Neither is
reachable from the published engine module, `go build ./...` at the repo root
never sees them, and the two talk over stdout JSON — no CGO, no linkage. The
fork itself stays pure Go, which CI asserts separately.
