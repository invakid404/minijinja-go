#!/usr/bin/env bash
#
# verify-seed.sh — prove this fork's engine tree is a mechanical derivation of
# upstream mitsuhiko/minijinja@$UPSTREAM_COMMIT (subdir minijinja-go/).
#
# It re-downloads the pinned upstream commit, replays the declared mechanical
# transform (module-path rewrite + vendored-fixture path constants), and diffs
# the result against this working tree.  Anything else — any semantic delta —
# shows up as a diff and fails the check.
#
# Files that are legitimately fork-only (provenance, CI, oracle) are declared in
# FORK_ADDED below; the script also fails if a fork-only file appears that is
# NOT declared, so nothing can be smuggled in unlisted.
#
# Usage: scripts/verify-seed.sh [--allow-semantic-delta]
#
#   --allow-semantic-delta  report differences but exit 0.  Use this once
#                           intentional semantic patches start landing (they are
#                           logged in PATCHES.md); until then the check is
#                           expected to be exactly clean.

set -euo pipefail

UPSTREAM_REPO="mitsuhiko/minijinja"
UPSTREAM_COMMIT="b9afca428b1c8149b1b3a5aab26a32d09744cd83"
UPSTREAM_TAG="minijinja-go/v2.16.0"
UPSTREAM_SUBDIR="minijinja-go"
# git tree sha of $UPSTREAM_SUBDIR at $UPSTREAM_COMMIT
UPSTREAM_TREE_SHA="10edf0cdd0a0b04fe3513464f7d1d1da51459096"

OLD_MODULE="github.com/mitsuhiko/minijinja/minijinja-go/v2"
NEW_MODULE="github.com/invakid404/minijinja-go/v2"

# Upstream Rust-side conformance corpora that the Go port's snapshot tests read.
# In the monorepo they live beside the Go module; here they are vendored.
RUST_TEST_DIRS=(inputs snapshots parser-inputs lexer-inputs)
VENDOR_PREFIX="testdata/upstream/minijinja/tests"

# Paths that exist in this repo but not upstream.  Directories end with '/'.
FORK_ADDED=(
  "UPSTREAM.md"
  "PATCHES.md"
  ".gitignore"
  ".github/"
  "scripts/"
  "oracle/"
  "${VENDOR_PREFIX}/"
  # Slice 6 (template sweep): the engine feature set BAML builds with, and the
  # tests that prove it.  PATCHES.md #2.
  "internal/parser/features.go"
  "internal/parser/features_test.go"
  "feature_gate_test.go"
  # Slice 6: the engine's one documented fault, pinned in the root module.
  # PATCHES.md #9.
  "engine_contract_test.go"
  # Slice 3: the error-form contract, pinned in the root module.  PATCHES.md #31.
  "numeric_contract_test.go"
  # Slice 3 (numeric core): the exact model of the engine's numeric core, and
  # the tests that pin it.  PATCHES.md #10-#30.
  "value/numeric.go"
  "value/numeric_test.go"
  # Slice 4 (coercion, containers, VM): the comparison, mapping and kind-order
  # rules ported from the target engine, and the tests that pin them.
  # PATCHES.md #32-#44.
  "value/coerce.go"
  "value/valuecmp.go"
  "value/orderedmap.go"
  "value/coercion_fork_test.go"
  "value/valuecmp_fork_test.go"
  "value/orderedmap_fork_test.go"
)

# Derived files this fork intentionally modifies, each with the PATCHES.md entry
# that explains it.  A modified file that is NOT listed here fails the check,
# and a listed file that is no longer modified fails it too, so the list can
# neither hide a change nor rot.
SEMANTIC_DELTA=(
  "internal/parser/parser.go"       # #2 statement gate, #8 message wording
  "internal/parser/parser_test.go"  # #2 gated corpus entries are asserted
  "internal/lexer/lexer.go"         # #3 Unicode whitespace trimming
  "internal/errors/error.go"        # #6 ErrCannotUnpack, #8 ErrUnknownMethod, #31 empty detail
  "internal_helpers.go"             # #6, #8 re-exports of the new kinds
  "state.go"                        # #2, #4-#8; #13, #14, #17, #18, #22
  "minijinja_test.go"               # #2 tests of removed statements
  "template_test.go"                # #2 inherited corpus asserts the gate
  "template_state_test.go"          # #2 block-based state tests
  "environment_api_test.go"         # #2 include-based tests
  "value/ops.go"                    # #10-#16, #18, #21 operators; #32-#35, #40, #44 comparison, containment, repetition
  "value/value.go"                  # #17, #19, #20 conversions and payloads; #36-#38, #42, #44 mapping, truthiness, subscripts
  "defaults.go"                     # #24 range argument conversion; #41 range error class
  "filters/filters.go"              # #23 int/abs payload dispatch; #36, #43 ordered mappings, reverse
  "tests/tests.go"                  # #25 odd/even/integer at i128 width
)

ALLOW_SEMANTIC_DELTA=0
if [[ "${1:-}" == "--allow-semantic-delta" ]]; then
  ALLOW_SEMANTIC_DELTA=1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> fetching ${UPSTREAM_REPO}@${UPSTREAM_COMMIT}"
curl -fsSL "https://codeload.github.com/${UPSTREAM_REPO}/tar.gz/${UPSTREAM_COMMIT}" \
  | tar xz -C "$WORK"
SRC="${WORK}/$(basename "${UPSTREAM_REPO}")-${UPSTREAM_COMMIT}"
[[ -d "${SRC}/${UPSTREAM_SUBDIR}" ]] || { echo "FAIL: ${UPSTREAM_SUBDIR}/ missing from upstream tarball"; exit 1; }

# ---------------------------------------------------------------------------
# Step 1: upstream subtree byte-check (independent of the tarball).
# Every blob sha in $UPSTREAM_SUBDIR must match the recorded upstream tree.
# ---------------------------------------------------------------------------
if command -v git >/dev/null 2>&1; then
  echo "==> checking downloaded subtree against upstream tree ${UPSTREAM_TREE_SHA}"
  (cd "${SRC}/${UPSTREAM_SUBDIR}" && git init -q . >/dev/null 2>&1 \
    && git add -A >/dev/null 2>&1 \
    && got="$(git write-tree)" \
    && rm -rf .git \
    && if [[ "$got" != "${UPSTREAM_TREE_SHA}" ]]; then
         echo "FAIL: downloaded subtree sha ${got} != recorded ${UPSTREAM_TREE_SHA}"
         exit 1
       fi)
fi

# ---------------------------------------------------------------------------
# Step 2: replay the declared mechanical transform onto a clean upstream copy.
# ---------------------------------------------------------------------------
EXPECTED="${WORK}/expected"
cp -R "${SRC}/${UPSTREAM_SUBDIR}" "$EXPECTED"

echo "==> replaying mechanical transform"

# 2a. module path rewrite: every reference to the upstream module path.
{ grep -rl -- "$OLD_MODULE" "$EXPECTED" 2>/dev/null || true; } | while read -r f; do
  LC_ALL=C sed -i.bak "s|${OLD_MODULE//./\\.}|${NEW_MODULE}|g" "$f" && rm -f "${f}.bak"
done

# 2b. fixture path constants: the Go port reads the Rust crate's corpora from a
#     monorepo sibling directory that does not exist in a standalone fork, so
#     the corpora are vendored and the three path constants repointed.
LC_ALL=C sed -i.bak 's|"\.\./minijinja/tests|"'"${VENDOR_PREFIX%/minijinja/tests}"'/minijinja/tests|g' \
  "${EXPECTED}/template_test.go" && rm -f "${EXPECTED}/template_test.go.bak"
for f in "${EXPECTED}/internal/parser/parser_test.go" "${EXPECTED}/internal/lexer/lexer_test.go"; do
  LC_ALL=C sed -i.bak 's|"\.\./\.\./\.\./minijinja/tests|"../../'"${VENDOR_PREFIX%/minijinja/tests}"'/minijinja/tests|g' "$f"
  rm -f "${f}.bak"
done

# 2c. vendored upstream Rust conformance corpora, verbatim.
mkdir -p "${EXPECTED}/${VENDOR_PREFIX}"
for d in "${RUST_TEST_DIRS[@]}"; do
  cp -R "${SRC}/minijinja/tests/${d}" "${EXPECTED}/${VENDOR_PREFIX}/${d}"
done

# ---------------------------------------------------------------------------
# Step 3: diff expected vs actual.
# ---------------------------------------------------------------------------
echo "==> diffing derived upstream tree against this repo"
status=0

# 3a. every derived file must exist here, byte-identical.
(cd "$EXPECTED" && find . -type f -print) | sed 's|^\./||' | LC_ALL=C sort > "${WORK}/expected.list"
while read -r rel; do
  if [[ ! -f "${REPO_ROOT}/${rel}" ]]; then
    echo "MISSING: ${rel}"
    status=1
    continue
  fi
  if [[ "$rel" == "README.md" ]]; then
    # README.md is allowed exactly one delta: a fork banner prepended ahead of
    # the derived upstream text. The upstream part must still be byte-identical,
    # so this is checked as a strict suffix rather than waved through.
    want_bytes="$(wc -c < "${EXPECTED}/README.md" | tr -d ' ')"
    if ! tail -c "$want_bytes" "${REPO_ROOT}/README.md" | cmp -s - "${EXPECTED}/README.md"; then
      echo "MODIFIED: README.md (the derived upstream text is not preserved verbatim as a suffix)"
      status=1
    fi
    continue
  fi
  if ! cmp -s "${EXPECTED}/${rel}" "${REPO_ROOT}/${rel}"; then
    declared_delta=0
    for allowed in "${SEMANTIC_DELTA[@]}"; do
      [[ "$rel" == "$allowed" ]] && declared_delta=1 && break
    done
    if [[ $declared_delta -eq 1 ]]; then
      echo "DECLARED DELTA: ${rel} (see PATCHES.md)"
      echo "$rel" >> "${WORK}/seen-delta.list"
      continue
    fi
    echo "MODIFIED: ${rel}"
    # `|| true`: diff exits non-zero when files differ, and pipefail would
    # otherwise abort the run on the first modified file instead of reporting
    # every delta.
    { diff -u "${EXPECTED}/${rel}" "${REPO_ROOT}/${rel}" | sed 's/^/    /' | head -40; } || true
    status=1
  fi
done < "${WORK}/expected.list"

# A declared delta that is no longer a delta is a stale declaration: the patch
# it names either landed upstream or was reverted, and PATCHES.md should say so.
touch "${WORK}/seen-delta.list"
for declared in "${SEMANTIC_DELTA[@]}"; do
  if ! grep -qxF "$declared" "${WORK}/seen-delta.list"; then
    echo "STALE DELTA DECLARATION: ${declared} is identical to upstream; remove it from SEMANTIC_DELTA"
    status=1
  fi
done

# 3b. every repo file must either be derived from upstream or be declared.
(cd "$REPO_ROOT" && find . \
  \( -name .git -o -name .jj -o -name target -o -name node_modules \) -prune -o \
  -type f -not -name '.DS_Store' -print) \
  | sed 's|^\./||' | LC_ALL=C sort > "${WORK}/actual.list"
while read -r rel; do
  grep -qxF "$rel" "${WORK}/expected.list" && continue
  declared=0
  for allowed in "${FORK_ADDED[@]}"; do
    if [[ "$allowed" == */ ]]; then
      [[ "$rel" == "$allowed"* ]] && declared=1 && break
    else
      [[ "$rel" == "$allowed" ]] && declared=1 && break
    fi
  done
  if [[ $declared -eq 0 ]]; then
    echo "UNDECLARED FORK FILE: ${rel}"
    status=1
  fi
done < "${WORK}/actual.list"

if [[ $status -eq 0 ]]; then
  echo "OK: engine tree matches ${UPSTREAM_REPO}@${UPSTREAM_COMMIT} (${UPSTREAM_TAG}) apart from declared deltas"
  echo "    mechanical: module path ${OLD_MODULE} -> ${NEW_MODULE}; vendored Rust corpora path constants"
  echo "    semantic:   ${#SEMANTIC_DELTA[@]} declared file(s), all logged in PATCHES.md"
  exit 0
fi

echo
echo "Semantic delta detected against the pinned upstream baseline."
if [[ $ALLOW_SEMANTIC_DELTA -eq 1 ]]; then
  echo "(--allow-semantic-delta: reporting only). Every delta above must be logged in PATCHES.md."
  exit 0
fi
echo "If intentional, log it in PATCHES.md with its corpus ID and re-run with --allow-semantic-delta."
exit 1
