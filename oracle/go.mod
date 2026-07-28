// Test-only differential-oracle module.
//
// It is a separate module on purpose: the published engine module
// (github.com/invakid404/minijinja-go/v2) must stay a minimal, pure-Go,
// dependency-free engine, and `go test ./...` at the repo root must not depend
// on a Rust toolchain. Nothing here is ever imported by a consumer of the fork.
module github.com/invakid404/minijinja-go/oracle

go 1.23

require github.com/invakid404/minijinja-go/v2 v2.16.0

replace github.com/invakid404/minijinja-go/v2 => ../
