# Intentional semantic deltas

Every deliberate behavioural difference between this fork and its upstream
baseline is recorded here, keyed by the differential corpus ID that pins it.

**The log is empty. As of `v2.16.0-baml.2` — the canonical baseline — this fork
has no semantic delta over `mitsuhiko/minijinja@b9afca` (`minijinja-go/v2.16.0`).**
The only differences are the two mechanical transforms described in
[UPSTREAM.md](UPSTREAM.md), which `scripts/verify-seed.sh` proves. The same is
true of the superseded `v2.16.0-baml.0` and `v2.16.0-baml.1`: all three publish
the same engine, and none carries a semantic patch.

## Rules

- **No entry, no patch.** A behavioural change that is not listed here is a bug,
  and `scripts/verify-seed.sh` will report it as an undocumented delta.
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
| _(none yet)_ | | | | | | |

## Known divergences not yet patched

The differential currently records 15 declared divergences from BAML's engine,
all classed `engine`. They are listed with their evidence in
`oracle/divergences.json` and summarized by surface below. None is patched in
this slice: slice 1 establishes authority, and the semantic repairs are slices
2-6 of the scope's plan.

| Slice | Corpus IDs |
| --- | --- |
| 2 — generic `value_cmp` API | `valuecmp/object-eq-string`, `valuecmp/string-eq-object`, `valuecmp/string-in-object-list`, `valuecmp/object-eq-object` |
| 3 — numeric core | `arith/int-add-above-2pow53`, `arith/int-mul-i64-edge`, `arith/rem-negative`, `arith/div-by-zero` |
| 4 — coercion and containers | `cmp/int-in-string`, `container/map-render-insertion-order`, `container/map-loop-insertion-order` |
| 5 — builtins and pycompat | `str/lower-dotted-capital-i`, `str/upper-sharp-s`, `err/go-only-urlencode-filter` |
| 6 — template and error surface | `err/syntax-incomplete-if` |
