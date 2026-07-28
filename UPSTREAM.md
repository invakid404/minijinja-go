# Provenance

This repository is an owned, pure-Go fork of the MiniJinja Go port, maintained
to reproduce the observable behaviour of BAML v0.223's minijinja stack. It is
not a general-purpose divergence from upstream: every intentional difference is
logged in [PATCHES.md](PATCHES.md) and pinned by a differential corpus row.

## Pins

| What | Value |
| --- | --- |
| Upstream source | `mitsuhiko/minijinja`, tag `minijinja-go/v2.16.0` |
| Upstream commit | `b9afca428b1c8149b1b3a5aab26a32d09744cd83` (2026-02-20, "Release 2.16.0") |
| Upstream subdirectory | `minijinja-go/` |
| Upstream subtree sha | `10edf0cdd0a0b04fe3513464f7d1d1da51459096` |
| Fork module path | `github.com/invakid404/minijinja-go/v2` |
| Baseline tag | `v2.16.0-baml.0` |
| License | Apache-2.0, upstream `LICENSE` preserved verbatim (upstream ships no `NOTICE`) |

### Behavioural target

| What | Value |
| --- | --- |
| BAML release | `BoundaryML/baml` commit `85247f452fe202300ec38d41fdc4035ea3020983` (v0.223) |
| BAML's Rust engine | `boundaryml/minijinja`, branch `value-cmp`, commit `8cfc770a5dffeda2de5b910d2b9f870d7edeff7c` |
| Engine delta over upstream | One commit: a generic `value_cmp` dispatch on `Object`, tried before ordinary equality and ordering, from both operand sides |
| Engine feature set | `default-features = false` plus `macros`, `builtins`, `debug`, `preserve_order`, `adjacent_loop_items`, `unicode`, `json`, `unstable_machinery`, `custom_syntax`, `deserialization`, `serde` (`BoundaryML/baml@85247f45` `engine/Cargo.toml:99-115`). `multi_template` and `loader` are deliberately **not** enabled. |

"Exact" means BAML v0.223's observable behaviour on the declared BAML surface:
rendered bytes, boolean result, value coercion, and success/error behaviour. It
does not mean matching arbitrary applications of generic minijinja that BAML
neither enables nor can load.

## Ownership boundary

This module is a **generic engine**. It carries no BAML types.

    owned minijinja-go/v2  <--- generic engine fixes and extensions (this repo)
             ^
             |
    a BAML v0.223 profile package in the consuming repository
       |- BAML environment and builtins
       |- prompt / constraint value lowering, enum, class, list, media objects
       |- ctx, _, enum globals, role and output helpers

The fork's job is to expose the generic mechanisms a BAML adapter needs — value
comparison dispatch and unknown-method dispatch — and to make its numeric,
coercion and value-model rules agree with the Rust stack. It must never
hard-code BAML enums, aliases or media.

## Seed fidelity

The engine tree is a **mechanical derivation** of upstream, with exactly two
transforms and no semantic changes:

1. **Module path.** `github.com/mitsuhiko/minijinja/minijinja-go/v2` →
   `github.com/invakid404/minijinja-go/v2`, across 51 files. Plain upstream
   repository links in `README.md` (LICENSE, discussions, issues, logo,
   COMPATIBILITY.md) are left pointing at `mitsuhiko/minijinja` on purpose:
   those are attribution, not module identity.

2. **Vendored upstream conformance corpora.** `template_test.go`,
   `internal/parser/parser_test.go` and `internal/lexer/lexer_test.go` read the
   Rust crate's fixtures from `../minijinja/tests/{inputs,snapshots,parser-inputs,lexer-inputs}`
   — a monorepo sibling directory that does not exist in a standalone fork, so
   those three suites hard-failed with "no input files found". The 445 fixture
   files are vendored verbatim from the same commit under
   `testdata/upstream/minijinja/tests/`, and the three path constants repointed.
   Without this the fork would silently lose its Rust-parity conformance suite:
   the seed goes from 123 passing tests to 336.

`scripts/verify-seed.sh` proves this. It downloads the pinned upstream commit,
checks the subtree against the recorded tree sha, replays both transforms, and
diffs the result against the working tree. It also fails on any file in the repo
that is neither derived from upstream nor in its declared fork-added allowlist,
so nothing can be added unlisted. It runs in CI on every push.

Once intentional semantic patches land, run it with `--allow-semantic-delta`;
every reported difference must have a `PATCHES.md` entry.

## Upstream merge and security-update procedure

1. **Re-pin.** Update `UPSTREAM_COMMIT`, `UPSTREAM_TAG` and `UPSTREAM_TREE_SHA`
   in `scripts/verify-seed.sh`, and the pins table above.
2. **Import the new baseline.** Take the new upstream `minijinja-go/` subtree
   verbatim as one commit, so the diff against the previous verbatim import is
   exactly upstream's own change and nothing else.
3. **Replay the mechanical transforms** (`scripts/verify-seed.sh` defines them;
   it is the executable specification, not the documentation).
4. **Re-apply the semantic patches** listed in `PATCHES.md`, one commit per
   entry, keeping the corpus IDs attached.
5. **Re-record the oracle** (`oracle/record.sh`) and run the differential.
   Upstream may have changed behaviour that a patch was compensating for: a
   ledger entry that has stopped diverging fails the run and must be removed
   deliberately.
6. **Re-run the full engine suite** and the CGO-free assertions.
7. **Tag** `v<upstream>-baml.<n>`.

For a security update the same procedure applies, but steps 1-3 may be shipped
as their own tag ahead of the semantic re-application if a patch needs rework:
an unpatched fork on a fixed upstream is a valid intermediate state, a patched
fork on a vulnerable upstream is not.

## Releases

Tags are immutable and never moved. `v2.16.0-baml.0` is the semantically
untouched baseline: an engine tree byte-identical to upstream except for the two
mechanical transforms above.

Later tags increment the `-baml.<n>` suffix as semantic patches land. Consumers
pin by tag; a `replace` directive is local and does not reach downstream users,
so it must never be used for production consumption.
