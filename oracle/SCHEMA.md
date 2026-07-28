# Corpus and outcome schema

`schema_version` is `1` everywhere. It is checked by the Rust harness
(`harness/src/main.rs`), the Go runner (`corpus.go`, `outcome.go`, `ledger.go`)
and the ledger, so a schema change can never silently compare two different
fixtures.

## Corpus (`corpus/*.json`)

Corpus files are lanes: `seed.json`, `template.json`. Every file is loaded, and
each has its own recording (see below), so a change to one lane cannot stale
another's. Row ids are unique across the whole set.

```json
{
  "schema_version": 1,
  "rows": [
    {
      "id": "valuecmp/object-eq-string",
      "surface": "value_cmp",
      "form": "expression",
      "source": "color == 'RED'",
      "profile": "stock",
      "inputs": [
        { "name": "color",
          "value": { "kind": "cmp_object", "canonical": "RED", "display": "Red" } }
      ],
      "expect": "boolean",
      "notes": "why this row exists"
    }
  ]
}
```

| field | required | meaning |
| --- | --- | --- |
| `id` | yes | Stable minimized identifier. Ledger entries and `PATCHES.md` deltas are keyed by it, so an id is **never reused** for a different case. |
| `surface` | no | Divergence surface: `arithmetic`, `comparison`, `string`, `container`, `value_cmp`, `environment`, `control`. |
| `form` | yes | `expression` wraps the source as `{{ expr }}`, the same shape BAML uses to evaluate a constraint predicate (`jinja_helpers.rs:67-94`). `template` uses the source verbatim. |
| `source` | yes | The expression or template. |
| `profile` | no | Environment. Defaults to `stock`. One of `stock`, `trim_blocks`, `lstrip_blocks`, `trim_lstrip`, `keep_trailing_newline` — all *engine configuration only*, set identically on both sides. The BAML v0.223 profile (globals, filters, pycompat) becomes another value here in a later slice. |
| `inputs` | no | Ordered list of named bindings. |
| `expect` | no | `bytes` (default), `boolean` or `error`. It selects boolean normalization and documents intent; outcomes are always compared in full regardless. |
| `notes` | no | Why the row exists and what it is probing. |

The template is always named `corpus.txt`, which keeps the default auto-escape
callback on "none" on both sides so escaping never silently colours a byte
comparison.

### Typed values

Types are **declared, not inferred from JSON**, so int/float and map ordering
survive into both runtimes unambiguously. The Go side keeps an int as a
`json.Number` until its kind is known, so a 64-bit payload is never routed
through `float64` on the way in.

| `kind` | payload | notes |
| --- | --- | --- |
| `int` | `value` | i64. Written as a JSON number; parsed exactly on both sides. |
| `float` | `value` | f64. |
| `bool` | `value` | |
| `string` | `value` | UTF-8. |
| `null` | — | |
| `list` | `items` | array of typed values |
| `map` | `entries` | array of `{key, value}`. **Order is part of the fixture**: BAML builds the engine with `preserve_order`. |
| `cmp_object` | `canonical`, `display` | A generic host object that answers the engine's comparison hook by `canonical` while displaying `display`. This is the generic shape of BAML's enum object — alias display, canonical-value identity — with no BAML types involved. |

`cmp_object` is what exercises BoundaryML's sole engine delta. In Rust it
implements `Object::value_cmp` (compare to a string by canonical value, delegate
to `custom_cmp` for objects). In Go it implements the closest thing the fork
offers, `value.ObjectWithCmp`, plus `fmt.Stringer` for display. Both sides
implement the *same* canonical-value semantics, so any difference the
differential reports about them is a statement about comparison dispatch, not
about the fixture.

## Harness output (`recorded/rust-<rev>-<lane>.json`)

```json
{
  "schema_version": 1,
  "provenance": {
    "engine_repo": "https://github.com/boundaryml/minijinja",
    "engine_branch": "value-cmp",
    "engine_rev": "8cfc770a5dffeda2de5b910d2b9f870d7edeff7c",
    "engine_features": ["macros", "builtins", "..."],
    "harness_version": "1",
    "os": "macos",
    "arch": "aarch64",
    "corpus_sha256": "..."
  },
  "results": [{ "id": "...", "outcome": { "status": "ok", "render": "3" } }]
}
```

`corpus_sha256` is the digest of the exact corpus bytes the run was produced
from. Replaying a recording against a different corpus is a hard error, and the
recording a corpus replays against is chosen by its file name, so the two cannot
drift apart silently.

`os`/`arch` are recorded because they matter: `int64(float64)` conversion was an
architecture-dependent source of behaviour in the parked evaluator work, so the
same corpus is expected to be run on both amd64 and arm64 while numeric fixes
are being made.

### Outcome

| `status` | fields | meaning |
| --- | --- | --- |
| `ok` | `render`, `boolean` | Rendered successfully. `render` is compared byte for byte. |
| `error` | `category`, `kind`, `message` | Failed. Only `category` is compared. |
| `panic` | `message` | The engine panicked. This is an outcome, not a crashed run. |
| `unsupported` | `message` | The runtime could not model the row. Any mismatch involving it is classified `harness-incomplete`, never as an engine divergence. |

Error categories are a canonical vocabulary shared by both implementations:
`syntax`, `undefined`, `unknown_filter`, `unknown_test`, `unknown_function`,
`unknown_method`, `invalid_operation`, `template_not_found`, `bad_escape`,
`unknown_block`, `missing_argument`, `too_many_arguments`, `bad_include`,
`out_of_fuel`, `eval_block`, `non_primitive`, `non_key`, `cannot_unpack`,
`bad_serialization`, `write_failure`, `other`.

One more category exists only on the Go side and is deliberately not smoothed
over: `unclassifiable` means the fork returned an error type an external
consumer cannot classify at all. It has no occurrences left — compile errors
were the last source of it (`PATCHES.md` #1) — and it stays in the vocabulary so
a regression would be named rather than absorbed. Four Rust kinds still have no
fork counterpart (`non_primitive`, `non_key`, `bad_serialization`,
`write_failure`); they would surface as a category divergence.

## Ledger (`divergences.json`)

```json
{
  "schema_version": 1,
  "entries": [
    {
      "id": "valuecmp/object-eq-string",
      "class": "engine",
      "summary": "one line stating the difference",
      "rust_signatures": ["ok|true|true"],
      "go_signatures": ["ok|false|false"],
      "slice": "2 BAML profile / value-model foundation"
    }
  ]
}
```

A *signature* is the compared part of an outcome: `ok|<boolean>|<render>` or
`error|<category>`. Both sides are recorded so a divergence that changes shape
fails instead of being absorbed.

`class` is one of `engine`, `profile`, `host`, `harness-incomplete` — see
`README.md` for how the three-way result assigns them.

### Architecture-dependent divergences

A side may list more than one accepted signature **only** when the entry also
sets `"architecture_dependent": true`. Loading the ledger fails otherwise, so a
signature list can never be used to quietly widen an entry until it stops
failing.

`arith/int-mul-i64-edge` is the current example, and it is why the field exists:

```json
"go_signatures": ["ok|-|9223372036854775807", "ok|-|-9223372036854775808"],
"architecture_dependent": true
```

The same Go source on the same input renders `9223372036854775807` on
darwin/arm64 and `-9223372036854775808` on linux/amd64 — saturation versus
wraparound in `int64(float64)`. The scope predicted exactly this class of
platform dependence, and the first cross-platform CI run surfaced it: the row
passed on macOS and failed on Linux as "divergence changed shape". That is the
reason the differential runs on both architectures rather than one.
