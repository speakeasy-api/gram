# Gateway Python runner

A dedicated executable and image for ephemeral gateway code. It uses only the
MIT-licensed OSS Monty runtime, pool and types, pinned to
`15753b35e8eee6d569f223cd19f0303dbf07f896`. It does not use Monty's commercial
server or host adapter, assistant sessions, a language model, or a workspace.

Each accepted stream gets a fresh Monty worker process and interpreter, installs
the small Python tools prelude, and evaluates one snippet. The worker is killed
on completion or interruption; it never serves a later execution. No execution
is resumed or automatically replayed. Persistent runner and yamux connections
amortize connection setup without retaining interpreter processes or state.

Build locally with `mise run build:monty` and `mise run build:code-runner`.
Run `mise run test:code-runner` and `mise run lint:code-runner`. Tests require a
real Monty executable; `--monty-bin` can point to an existing build of the
pinned revision. `cargo test` does not silently skip runtime tests when it is absent.
Rust 1.96.1 is pinned for development; the image uses the repository's pinned
Rust 1.97 build base. The existing assistant crate retains its own toolchain.

Start `agents/code-runner/target/debug/gram-code-runner` with
`--monty-bin agents/code-runner/target/monty/bin/monty`. Supply a random bearer
secret of at least 32 bytes in `GRAM_CODE_RUNNER_TOKEN`; it is never exposed to
Python or process arguments. Linux startup disables parent process inspection.
The default listen address is `127.0.0.1:8081`. The dedicated Docker image
listens on port 8081 and runs as UID 65532. Build it from the repository root:
`docker build -f agents/code-runner/Dockerfile -t gram-code-runner:dev .`.

## Transport contract, version 1

Gram initiates `GET /v1/connect` with `Authorization: Bearer <runner secret>`,
`Connection: upgrade`, `Upgrade: gram-code-yamux`, and `Gram-Code-Protocol: 1`.
The runner verifies authentication and version before returning 101. The upgraded
byte stream carries yamux, with Gram as client and the runner as server. One
stream owns one execution. `/health` returns the runtime and protocol version.
Plain HTTP is intended for the private sandbox network or loopback. Externally
routed connections require TLS termination and authenticated ingress.

Each frame is a four-byte big-endian length followed by UTF-8 JSON, limited to
1 MiB plus 4 KiB of envelope. `src/wire.rs` defines the exact tagged messages:

1. Gram sends `start` with `version: 1`, a UUID `execution_id`, `code`, and
   `wall_ms` between 1 and 30000. The runner either refuses admission or sends
   `started`. IDs are retained for two minutes in a bounded replay window;
   callers must mint a new ID for each deliberate user execution and never
   retry a snippet automatically, including after that window expires.
   A lost connection after sending `start` is indeterminate even if `started`
   was not received. Only `admission_refused` proves code did not run.
2. The runner emits `callback` with a unique integer `id`, `method` and JSON
   `arguments`. Methods are `search`, `describe`, `call`, and `servers`.
3. Gram answers once with `callback_result`, the same `id`, and either `value`
   or a bounded public `error`. Replies can arrive out of order. Python receives
   JSON-compatible values, or a `RuntimeError` for a helper refusal.
4. The runner sends `complete` with a JSON `value`, captured `output` and
   `output_truncated`; otherwise it sends a terminal `error`.

`cancel`, stream EOF, connection loss, or the wall deadline destroys the current
interpreter. Gram must also cancel its outstanding upstream requests and treat
already-dispatched side effects as potentially unknown. Closing a stream does
not roll those effects back. The protocol contains no project, tool destination,
credentials, arbitrary OS callback, snapshot or resume operation.
Gram owns callback dispatch records and derives unknown outcomes from them for
every terminal error or connection loss. SIGTERM stops admissions and drains
active streams for up to 31 seconds before forcing cancellation.
Queued callbacks are discarded once execution ends. A callback already being
written finishes its frame; Gram must check its own deadline/cancellation before
dispatch and ignore callbacks received after it. Sandbox termination grace must
be at least 35 seconds to cover execution, terminal-frame and connection draining.

## Initial bounds

- 64 KiB source; 30 seconds wall time; 2 seconds active Monty execution.
- 64 MiB interpreter allocation; recursion depth 128; 256 suspensions.
- 64 helper attempts and at most eight unresolved callbacks per execution.
- 1 MiB per helper result; 8 MiB cumulative callback frames.
- 256 KiB final JSON; 16 KiB captured print output, truncated at a UTF-8 boundary.
- Four executions by default, configurable from 1 to 16. Saturation refuses
  admission. At most 32 connections, 16 streams and 4 MiB receive window per
  connection; five seconds to receive headers and the first execution frame.

JSON conversion rejects cyclic/deep values, non-string dictionary keys,
non-finite numbers, integers larger than 4096 bits and unsupported Python objects.
The integer bound is checked before expensive decimal conversion. There are no filesystem
mounts, host networking helpers or credentials. gVisor and container limits add
the outer isolation boundary in the dedicated sandbox deployment; local tests
alone do not establish that boundary or production capacity.

The tests exercise fresh state, discovery/calls, concurrent futures, filesystem
denial, allocator/time/output limits, cancellation, deadlines, framing, duplicate
IDs, authentication and multiplexing against real Monty subprocesses. This
runner layer does not expose a public MCP tool or change management settings.
