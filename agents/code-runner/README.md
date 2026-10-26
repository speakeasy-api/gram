# Gateway Python runner

A dedicated executable and image for ephemeral gateway code. It uses only the
MIT-licensed OSS Monty runtime, pool and types, pinned to
`15753b35e8eee6d569f223cd19f0303dbf07f896`. It does not use Monty's commercial
server or host adapter, assistant sessions, a language model, or a workspace.

Each accepted stream gets a fresh Monty worker process and interpreter, installs
the small Python tools prelude, and evaluates one snippet. The worker is killed
on completion or interruption; it never serves a later execution. No execution
is resumed or automatically replayed. The worker pool keeps one pre-started idle
worker warm (`min_processes = 1`, up to the runner's execution capacity) so an
execution does not wait for process startup; that worker has never run code, and
each worker serves exactly one execution (`max_checkouts_per_worker = 1`).
Persistent runner and yamux connections amortize connection setup; no
interpreter state survives an execution.

Build locally with `mise run build:monty` and `mise run build:code-runner`.
Run `mise run test:code-runner` and `mise run lint:code-runner`. Tests require a
real Monty executable; `--monty-bin` can point to an existing build of the
pinned revision. `cargo test` does not silently skip runtime tests when it is absent.
Rust 1.96.1 is pinned for development and in both image build stages. The
existing assistant crate retains its own toolchain.

Start `agents/code-runner/target/debug/gram-code-runner` with
`--monty-bin agents/code-runner/target/monty/bin/monty`. Supply a random bearer
secret of at least 32 bytes in `GRAM_CODE_RUNNER_TOKEN`; it is never exposed to
Python or process arguments. Linux startup disables parent process inspection.
In the image, `GRAM_CODE_RUNNER_MONTY_BIN` points at `gram-code-worker`, which
confines each worker before exec'ing the `monty` binary beside it:

- its own Landlock domain allowing only reading and executing beneath `/usr`,
  `/lib`, `/lib64`, `/bin`, `/etc`, `/dev` and the shim's directory, so `/proc`,
  `/sys` and `/tmp` are out of reach. A kernel without Landlock (`ENOSYS` or
  `EOPNOTSUPP`), such as gVisor, skips this layer, and the sandbox then bounds
  the worker;
- a seccomp filter denying cross-process syscalls and the creation of child
  processes, while still allowing threads;
- a two-second CPU-time limit (`RLIMIT_CPU`).

An escaped worker cannot read, trace or signal a sibling, or start a process
(`tests/worker_isolation.rs`, Linux only). Each worker serves one execution and
its threads share the CPU limit, so it is a per-execution budget: a program
that uses it fails with `Python CPU time limit exceeded`, while CPU contention
on a busy pod only slows programs down. Pointing `--monty-bin` straight at
`monty`, as above and on macOS, where the shim does not run, applies none of
these: no worker isolation and no CPU budget, only Monty's own limits. To
apply them locally on Linux, point `--monty-bin` at
`target/debug/gram-code-worker` with a copy of `monty` beside it.
The default listen address is `127.0.0.1:8081`. The dedicated Docker image
listens on port 8081 and runs as UID 65532. Build it from the repository root:
`docker build -f agents/code-runner/Dockerfile -t gram-code-runner:dev .`.

## Transport contract, version 1

Speakeasy initiates `GET /v1/connect` with `Authorization: Bearer <runner secret>`,
`Connection: upgrade`, `Upgrade: gram-code-yamux`, and `Gram-Code-Protocol: 1`.
The runner verifies authentication and version before returning 101. The upgraded
byte stream carries yamux, with Speakeasy as client and the runner as server. One
stream owns one execution. `/health` returns the runtime and protocol version.
Plain HTTP is intended for the private sandbox network or loopback. Externally
routed connections require TLS termination and authenticated ingress.

Each frame is a four-byte big-endian length followed by UTF-8 JSON, limited to
1 MiB plus 4 KiB of envelope. `src/wire.rs` defines the exact tagged messages:

1. Speakeasy sends `start` with `version: 1`, a UUID `execution_id`, `code`, and
   `wall_ms` between 1 and 30000. The runner either refuses admission or sends
   `started`. IDs are retained for two minutes in a bounded replay window;
   callers must mint a new ID for each deliberate user execution and never
   retry a snippet automatically, including after that window expires.
   A lost connection after sending `start` is indeterminate even if `started`
   was not received. Only `admission_refused` and `capacity_exceeded` prove
   code did not run; both are sent before the execution is admitted.
2. The runner emits `callback` with a unique integer `id`, `method` and JSON
   `arguments`. Methods are `search`, `describe`, `call`, and `servers`.
3. Speakeasy answers once with `callback_result`, the same `id`, and exactly one of
   `value` or a bounded public `error`; a reply with both or neither ends the
   execution. Replies can arrive out of order. Python receives
   JSON-compatible values, or a `RuntimeError` for a helper refusal.
4. The runner sends `complete` with a JSON `value`, captured `output` and
   `output_truncated`; otherwise it sends a terminal `error`.

`cancel`, stream EOF, connection loss, or the wall deadline destroys the current
interpreter. Speakeasy must also cancel its outstanding upstream requests and treat
already-dispatched side effects as potentially unknown. Closing a stream does
not roll those effects back. The protocol contains no project, tool destination,
credentials, arbitrary OS callback, snapshot or resume operation.
Speakeasy owns callback dispatch records and derives unknown outcomes from them for
every terminal error or connection loss. SIGTERM stops admissions and drains
active streams for up to 31 seconds before forcing cancellation.
Cancellation and the deadline discard queued callbacks. A callback already being
written finishes its frame; Speakeasy must check its own deadline/cancellation before
dispatch and ignore callbacks received after it. A run that fails on its own,
such as at the concurrency limit, first delivers every callback it already
issued, ahead of its terminal `error`, so Speakeasy can record each call. Sandbox termination grace must
be at least 35 seconds to cover execution, terminal-frame and connection draining.

## Initial bounds

- 64 KiB source; 30 seconds wall time; 2 CPU-seconds per execution through the
  worker shim, with Monty's 25-second active-execution limit as a backstop.
- 64 MiB interpreter allocation; recursion depth 128; 256 suspensions.
- 64 helper attempts and at most eight unresolved callbacks per execution; a
  ninth ends the run with `execution_failed` and is never sent.
- 1 MiB per helper result; 8 MiB cumulative callback arguments and results.
- 256 KiB final JSON; 16 KiB captured print output, truncated at a UTF-8 boundary.
- Twelve executions by default (`GRAM_CODE_RUNNER_CAPACITY`), configurable
  from 1 to 16. The upgrade response reports it in `Gram-Code-Capacity`, and
  the server opens at most that many streams per pod. Saturation refuses with
  `capacity_exceeded`. At most 32 connections, 16 streams and 4 MiB receive window per
  connection; five seconds to receive headers and the first execution frame.
  At most 16 streams buffer an unadmitted first frame at once; a stream that
  cannot start reading, or does not deliver its `start` frame, within those
  five seconds is refused admission.

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
