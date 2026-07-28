# The differential oracle

One corpus, two engines, one comparison.

```
oracle/corpus/seed.json ──┬──> oracle/harness (Rust)  ─> boundaryml/minijinja@8cfc770  ─┐
                          │                                                            ├─> compare ─> oracle/divergences.json
                          └──> oracle (Go)            ─> this fork                     ─┘
```

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

`recorded/rust-8cfc770.json` is a committed run of the harness. It carries the
SHA-256 of the corpus bytes it was produced from, so it cannot be replayed
against a corpus it does not correspond to — a stale recording is a hard error,
never a quietly weaker test.

## The comparison contract

Both sides produce the same `Outcome` shape, and only part of it is compared:

| field | compared | why |
| --- | --- | --- |
| `status` | yes | ok / error / panic / unsupported are different outcomes |
| `render` | yes, byte for byte | prompt parity is a bytes question |
| `boolean` | yes, when the row expects one | BAML decides a constraint by parsing `true`/`false` |
| `category` | yes | canonical error vocabulary shared by both sides |
| `kind`, `message` | **no** | two engines are not expected to word an error identically; pinning text would drown real divergences in noise |

A panic is an outcome, not a crashed run. So is "the harness cannot model this
row" (`unsupported`), which is what makes `harness-incomplete` distinguishable
from a real engine difference instead of being silently scored as one.

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
to be closed. Nothing here is fixed in slice 1: this slice establishes
authority, and the semantic repairs are slices 2-6.

## Seed corpus

25 rows spanning arithmetic, comparison, string, container, `value_cmp` object,
environment and control surfaces, in both expression and full-template form.
It is deliberately small — enough to prove the plumbing end to end and to pin
one known divergence per class. The full corpus is a later slice.

Ten rows agree. Fifteen diverge, all declared. The `value_cmp` rows are the
generic form of BAML's #597 enum fence: a host object with a canonical
comparison identity (`RED`) and a different display (`Red`). Its display row
agrees on both engines, which is what isolates the remaining four rows to
comparison dispatch rather than to the fixture.

One of the fifteen, `arith/int-mul-i64-edge`, is **architecture-dependent**: the
fork renders `9223372036854775807` on darwin/arm64 and `-9223372036854775808`
on linux/amd64 for the same input. The first cross-platform CI run found it,
because the ledger refuses a divergence that changed shape. That is why the
differential runs on both architectures.

## Rust is test-only

The harness is a separate Cargo project and the runner is a separate Go module
(`github.com/invakid404/minijinja-go/oracle`) with a local `replace`. Neither is
reachable from the published engine module, `go build ./...` at the repo root
never sees them, and the two talk over stdout JSON — no CGO, no linkage. The
fork itself stays pure Go, which CI asserts separately.
