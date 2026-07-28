# Gram Assistant Runner

Rust HTTP runner for assistant threads. The crate is intentionally standalone so
it can be built and tested without the Go/TypeScript workspaces.

## Setup

The crate pins Rust `1.95.0` via `rust-toolchain.toml`.

Common commands:

```sh
mise run build:assistant-runner
mise run lint:assistant-runner
mise run test:assistant-runner
```

From `agents/runner`, the same CI-oriented Cargo aliases are available:

```sh
cargo ci-check
cargo ci-clippy
cargo ci-test
```

`cargo ci-clippy` runs all targets and features with `-D warnings`; keep new
test-only `unwrap`, `expect`, and `panic` usage scoped behind local lint
allowances.
