#!/usr/bin/env bash
# Rebuild the Rust oracle harness and refresh the committed recording of its
# output. Run this after any change to oracle/corpus/*.json or to the harness.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
exec go run ./cmd/oracle record
