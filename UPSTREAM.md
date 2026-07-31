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
| Baseline tag | `v2.16.0-baml.2` — the semantically untouched baseline every patch is a delta over (see [Releases](#releases)) |
| Current release | `v2.16.0-baml.4` |
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
transforms, plus the semantic patches declared in
[PATCHES.md](PATCHES.md) (none at the `v2.16.0-baml.2` baseline; the template
sweep was the first to add any):

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

Semantic patches have landed since, so the script also carries a
`SEMANTIC_DELTA` list: the derived files this fork intentionally modifies. A
modified file that is not on it fails the check, and a listed file that no
longer differs fails it too, so the declaration can neither hide a change nor
rot. Every entry on that list is explained by a `PATCHES.md` row with the corpus
ID that pins it. `--allow-semantic-delta` remains as an escape hatch for a
work-in-progress tree.

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

Tags are immutable and never moved, including superseded ones. Consumers pin by
tag; a `replace` directive is local and does not reach downstream users, so it
must never be used for production consumption.

The `-baml.<n>` suffix increments on every published release of the fork against
the same upstream version, whether or not that release carries a semantic patch.
`PATCHES.md` — not the suffix — is what says whether a release has a semantic
delta.

Superseding rather than moving is the rule: a published tag that turns out to be
wrong gets a successor, never a rewrite. Every tag below still resolves.

| Tag | Status | Notes |
| --- | --- | --- |
| `v2.16.0-baml.4` | **current** | Slice 7: the opaque host map (equality directionality, the ordering fault, and iterating a mapping that cannot be enumerated) and `ObjectWithString` dispatch through `Value.String`/`Value.Repr`. `PATCHES.md` #102-#105, pinned by the new `oracle/corpus/opaque.json` lane. |
| `v2.16.0-baml.3` | retained, superseded | Slice 5: the builtin registry, string/format/JSON operations and the pycompat method surface. `PATCHES.md` #45-#101. Superseded by `-baml.4`, which is the same engine plus slice 7. |
| `v2.16.0-baml.2` | retained, superseded | The **semantically untouched baseline**: upstream `minijinja-go/v2.16.0` with no engine change at all. It remains the reference `scripts/verify-seed.sh` derives from, and every patch in `PATCHES.md` is a delta over it. All CI green on linux/amd64 and darwin/arm64 at the tagged commit. |
| `v2.16.0-baml.1` | retained, superseded | Green, but its copy of this file claimed the published module zips were identical across tags. They are not: they also differ in the provenance markdown, as the table note below records. Superseded to keep the provenance accurate about its own artifacts. |
| `v2.16.0-baml.0` | retained, superseded | Its `oracle` workflow is red on linux/amd64: it predates the explicit architecture-dependent ledger handling, so `arith/int-mul-i64-edge` fails there as a shape change. Retained unmoved because it is already in the Go checksum database. |

Tags `-baml.0` through `-baml.2` publish **the same engine**. Comparing their
module zips, the only files that differ are `README.md`, `UPSTREAM.md` and
`PATCHES.md` — the provenance documents themselves, which each release updates
to record the supersession. No Go source, no test data, and no engine behaviour
differs between them. `oracle/` does not enter the comparison at all: it is a
separate module and is excluded from every module zip.

This is checkable rather than asserted:

```
$ unzip -q .../v2.16.0-baml.1.zip -d z1 && unzip -q .../v2.16.0-baml.2.zip -d z2
$ diff -rq z1/...@v2.16.0-baml.1 z2/...@v2.16.0-baml.2
Files PATCHES.md and PATCHES.md differ
Files README.md and README.md differ
Files UPSTREAM.md and UPSTREAM.md differ
```

That claim stops at `-baml.2`. `-baml.3` and `-baml.4` carry real engine
changes, each declared in `PATCHES.md` and pinned by a differential corpus row,
so their zips differ in Go source as well.
