---
name: gram-pubsub-python
description: |
  How to build and run GCP Pub/Sub stream subscribers in Python under `pystreams/` — the `multi` command (start at `pystreams/src/pystreams/cmd/multi.py`), the `gram_infra.pubsub` publisher/subscriber library, and the anyio runtime mirroring the Go `gram streams` process. Activate for ANY Python Pub/Sub work in Gram: adding or changing a `pystreams` subscriber/handler, NLP/ML scanning consumers (Presidio PII detection, prompt-injection classifiers, spaCy/transformer models over streams), registering a receiver, the local emulator for pystreams, or deciding whether a new consumer belongs in Go `streams` or Python `pystreams` — including phrasings like "add a Presidio scanner", "consume this topic in Python", "wire up an ML handler", or "why do we have Python in this monorepo" even when "pystreams" isn't named. Topic/subscription DECLARATION (proto options, `kcc.yaml`) lives in the `gram-pubsub` skill; use THIS skill for the Python runtime that consumes them.
metadata:
  relevant_files: "pystreams/src/pystreams/**/*.py, pystreams/tests/**/*.py, pystreams/pyproject.toml, pystreams/Dockerfile.pystreams, infra/gram_infra/pubsub/*.py, infra/proto/**/*.proto, .mise-tasks/start/pystreams-multi.sh, .mise-tasks/test/pystreams.sh, .mise-tasks/lint/pystreams.sh"
---

# Gram Pub/Sub in Python (`pystreams`)

`pystreams` is the Python home for Pub/Sub stream consumers in the Gram
monorepo. It runs alongside — not instead of — the Go `gram streams` process,
and it consumes the **same** proto-declared Pub/Sub topology. This skill covers
the Python runtime: how a subscriber is written, registered, and run. For
declaring the topic/subscription itself (the `(gcp.pubsub.v1.topic)` /
`(gcp.pubsub.v1.subscription)` proto options and `infra/gen/kcc.yaml`), use the
**`gram-pubsub`** skill — that half is language-agnostic and shared.

## Why Python exists here, and when to reach for it

The original and default stream-processing home is the Go **streams component**
(`server/cmd/gram/streams.go`). Go's concurrency model and the GCP Pub/Sub Go
SDK's use of it are substantially better suited to high-throughput message
fan-out than the Python equivalent, async Python included. So the rule of thumb
is: **build new consumers in Go `streams` by default.**

`pystreams` exists for the cases where Python's _ecosystem_ is the deciding
factor — overwhelmingly working with language models, transformers, NLP/ML
use cases. Presidio (PII detection), prompt-injection classifier models, spaCy,
and the transformer tooling these depend on are Python-first and have no
comparable Go story. Reaching into that ecosystem from Go would mean
reimplementing or shelling out; running the consumer in Python is the honest
path. So the decision is not "which language do I prefer" but "does this
consumer need a Python-only library to do its job?" If yes, `pystreams`; if no,
Go `streams`.

The first real `pystreams` consumer is the Presidio PII scanner
(`pystreams/src/pystreams/risk/handler.py`); `ping` is a heartbeat used to keep
the publish→subscribe path exercised, mirroring the Go `ping`.

## How the pieces fit

```
infra/proto/**.proto            topic/subscription declarations (shared, see gram-pubsub)
  └─ buf generate ─┬─ infra/gen/**        Go types  + infra/gen/kcc.yaml
                   └─ infra/gen_py/**      Python types (gram.*.v1.*_pb2)

infra/gram_infra/pubsub/        the Python publisher/subscriber library
  ├─ broker.py                  PubSubBroker / EmulatedPubSubBroker (mirrors infra/pkg/gcp)
  ├─ publisher.py               Publisher[M], pubsub_publisher_for_message
  ├─ subscriber.py              Subscriber[M], pubsub_subscriber_for_message
  └─ discover.py                proto-option → resource-name resolution

pystreams/                      the runnable service ("multi" command)
  └─ src/pystreams/cmd/multi.py the entrypoint — START HERE
```

`infra/gram_infra` (the `gram-infra` package) is a uv workspace member shared by
`pystreams`; it holds both the generated Python protobufs and the hand-written
Pub/Sub convenience layer. `pystreams` is the deployable that wires handlers
onto subscriptions and runs the receive loops. Read the library as the Python
counterpart of `infra/pkg/gcp/` — the docstrings deliberately point back to the
Go files they mirror, so the two layers stay behaviorally aligned.

## Read `multi.py` first

`pystreams/src/pystreams/cmd/multi.py` is the whole service in one screen. It is
a Click command (`multi`) that `anyio.run`s an async `multi(...)` coroutine.
Walking it top to bottom teaches the runtime:

1. **Configure logging** (structlog) with service attributes.
2. **Pick a project id**: real `--gcp-project-id`, or a throwaway when an
   emulator host is set (the emulator doesn't care about the project).
3. **Build a broker** (`_build_broker`): `EmulatedPubSubBroker` when
   `--pubsub-emulator-host` is set (it _reconciles_ topics/subscriptions on
   demand, since the emulator has no Config Connector), else `PubSubBroker`
   (assumes Config Connector already provisioned everything). This is the exact
   prod-vs-local split the Go layer makes.
4. **Enter the broker as a context manager** (`with broker:`) — it owns the
   publisher/subscriber clients and flushes + closes them on exit, including a
   clean Ctrl-C teardown.
5. **Open one anyio task group** and start, in order: a signal handler that
   cancels the group on SIGINT/SIGTERM, the event-loop-lag monitor, the health
   server (awaited so it's bound before consuming begins), then the
   `ReceiverGroup`.
6. **Register receivers**, one `receivers.receive(...)` call per subscription.
7. **Flip readiness on** (`health_state.set_ready()`) only after receivers are
   wired, so `/readyz` doesn't go green before the service can actually consume.

The structured-concurrency shape matters: everything runs as a child task of
that single task group, so any fatal error or a shutdown signal tears the whole
process down together for a clean restart — the Python analogue of the Go
`errgroup` in `streams.go`.

## Adding a subscriber — the two steps

Exactly like the Go side: **write a handler, then register it.**

### Step 1 — write an async handler

A handler is an async callable `(message, meta) -> None`. The proto message type
is the topic's payload; `meta` is `gram_infra.pubsub.subscriber.MessageMetadata`
(`id`, `attributes`, `delivery_attempt`). The **return/raise is the ack/nack
signal**, identical to Go: returning normally **acks**; raising **nacks**
(triggering redelivery and eventual dead-lettering if the subscription declares
a `dead_letter` policy). You never call `ack()`/`nack()` yourself in the
callback form — the library does it from your handler's outcome.

The minimal shape is `PingHandler` (`pystreams/src/pystreams/ping/handler.py`):
a class holding its dependencies, with an `async def handle(self, message, meta)`.
Register `handle` (the bound method), not the class.

The real reference is `PresidioHandler` (`pystreams/src/pystreams/risk/handler.py`).
It demonstrates the patterns that matter for ML/NLP consumers:

- **Load the model once, reuse across messages.** Constructing Presidio's
  `AnalyzerEngine` loads a spaCy model — expensive. Do it in `__init__`, not per
  delivery.
- **Run CPU-bound work off the event loop.** This is _the_ Python-specific
  hazard. anyio runs everything on one event-loop thread; a synchronous,
  CPU-bound call (model inference, regex over large text) that doesn't await
  will stall _every other subscription_ until it returns. Wrap such work with
  `asyncer.asyncify(...)` so it runs in a worker thread and the loop stays
  responsive. The loop-lag monitor exists precisely to make this kind of stall
  visible before it becomes an outage.
- **Make heavy dependencies injectable behind a `Protocol`.** `PresidioHandler`
  depends on a narrow `Analyzer` protocol, so tests inject a lightweight fake
  instead of loading the NLP model (see `pystreams/tests/test_risk_handler.py`).
- **Decide ack/nack deliberately, especially when no DLQ policy is declared.**
  The `PresidioScanner` subscription declares no `dead_letter` policy, so a raised
  exception would nack and redeliver the _same_ message for the full retention
  window — one poison input could loop for 30 days. Because this is best-effort
  shadow processing, the handler **swallows `Exception` and returns** (acking)
  rather than poisoning the subscription, logging the error _type_ only. Catch
  `Exception`, never `BaseException`, so cancellation (graceful shutdown) still
  propagates. Match the policy to intent: raise only when you genuinely want the
  message retried.
- **Never log PII or sensitive data.** PII/security handlers log entity _types_
  and counts, request ids, and delivery attempts — never the text or the
  matches. An error string or traceback can echo the input, which is why the
  failure path logs `type(exc).__name__` and not the exception message.

### Step 2 — register it in `multi.py`

Add a `receivers.receive(...)` call in the marked block, mirroring the topic and
subscription proto declarations:

```python
receivers.receive(
    presidio_request_pb2.PresidioRequest,  # the TOPIC message type → fixes the handler's `message` type
    presidio_scanner_pb2.PresidioScanner,  # the SUBSCRIPTION marker message (carries the subscription option)
    PresidioHandler(logger).handle,        # your async handler callback
)
```

The three arguments line up one-to-one with the proto options the `gram-pubsub`
skill describes: the topic-declaring message, the subscription-declaring marker,
and the handler. `ReceiverGroup.receive` (`pystreams/src/pystreams/cmd/receiver.py`)
resolves a `Subscriber` via `pubsub_subscriber_for_message`, wraps your handler
in per-message tracing (`deps/tracing.py` — a `stream.handleMessage` span tagged
with the topic/subscription proto names, continuing the publisher's trace via
W3C context), and starts the receive loop as a child task. It's the direct
analogue of `mustReceive`/`receiverGroup` in `streams.go`, so each subscription
is one line at the call site.

> Go and Python can consume the _same_ topic through _separate_ subscriptions:
> `gram.ping.v2.Message` has both a `Processor` marker (consumed by Go) and a
> `PyProcessor` marker (consumed by `pystreams`). If you want a Python consumer
> of a topic the Go side already reads, declare a new subscription marker rather
> than stealing the existing one. Declaration is a `gram-pubsub` task.

### What the receive loop gives you for free

`Subscriber.receive` (`infra/gram_infra/pubsub/subscriber.py`) handles the
hard parts so handlers stay tiny: it unmarshals the payload into a fresh proto
instance (a malformed payload is nacked and never reaches you), runs handlers
concurrently up to a bounded `max_concurrency`, isolates a raised handler error
to that one message (logged with context, then nacked), bridges the
google-cloud-pubsub library's background threads onto the event loop without
parking a worker thread per subscriber, and tears down cleanly on
cancellation — nacking anything buffered-but-undispatched so the broker
redelivers immediately instead of waiting out the ack deadline. There is also a
`Subscriber.stream()` async-iterator form for explicit per-message ack/nack, but
`receive` (the callback form) is what `pystreams` uses; prefer it.

## Publishing from Python (rare)

Most events are published by the Go server where they originate. When Python
does need to publish, use `pubsub_publisher_for_message(broker, MessageType)` →
`await publisher.publish(msg)` (`infra/gram_infra/pubsub/publisher.py`). It
proto-marshals the body and tags it with `content-type: application/x-protobuf`
and `schema: <proto full name>` — the _same_ two attributes the Go publisher
sets — so messages are interoperable across languages, and it injects W3C trace
context so a Python→Python or Go→Python hop continues the trace. `publish` is a
coroutine; await it.

## Local development

`pystreams` runs locally against the shared Pub/Sub emulator — no Config
Connector, no real GCP.

- **Start it:** `pitchfork start pystreams-multi` or the pitchfork mcp server if
  available (runs `uv run multi` in the `pystreams/` dir).
- **Emulator:** `mise.toml` sets `PUBSUB_EMULATOR_HOST` (and the
  `pubsub-emulator` compose service the Go side uses is the same one). With that
  env var set, `multi` builds an `EmulatedPubSubBroker`, which lazily creates the
  topic and subscription on first use — so you don't provision anything locally.
- **Health/control server:** binds `GRAM_PYSTREAMS_CONTROL_HOST/PORT` (default
  `127.0.0.1:8089`), serving `GET /healthz` (liveness, always 200) and
  `GET /readyz` (503 until receivers are wired, then 200; flips back to 503 the
  instant shutdown begins so a rolling deploy drains in-flight handlers).
- **Env vars:** service config (`GRAM_SERVICE_VERSION`, `GRAM_ENVIRONMENT`,
  `GRAM_LOG_LEVEL`, `GRAM_LOG_PRETTY`) and GCP config (`GRAM_GCP_PROJECT_ID`,
  `PUBSUB_EMULATOR_HOST`) all have CLI flags too; see `cmd/flags_*.py`.

## Testing and linting

- **Tests:** `mise run test:pystreams` (pytest, `asyncio_mode = "auto"` so async
  tests need no decorator). Tests exercise handlers with fakes/protocols and the
  `structlog` capture helper — **no live broker or model load.** Follow that:
  inject a fake analyzer/dependency and assert on captured logs and ack/nack
  behavior. (Per repo convention, the broker/subscriber library tests also run
  under anyio's trio backend, not just asyncio, to keep them backend-agnostic.)
- **Lint/type-check:** `mise run lint:pystreams` runs `ty check` and
  `pyrefly check`. Both are strict; keep handlers fully typed.
- **Format:** `hk fix` formats changed files (Python included) across the branch.

## Generated protobufs

`mise run gen:infra` runs `buf generate`, which emits **both** Go (`infra/gen/`)
and Python (`infra/gen_py/`, via the `protocolbuffers/python` + `pyi` plugins).
Import the Python types as `from gram.<pkg>.v1 import <name>_pb2` and the library
as `from gram_infra.pubsub import ...`. After any change to a `.proto` under
`infra/proto/`, regenerate and commit per the `gram-pubsub` skill — the Python
side picks up the new `_pb2` modules from the same run. The `protobuf` runtime
floor is pinned to match buf's generator version (see `infra/pyproject.toml`);
keep them in lockstep if the buf plugin version changes.

## Observability

Tracing, logging, and the loop-lag metric are wired through OpenTelemetry's API
and structlog. The per-message span and W3C propagation mirror the Go receiver
exactly (`deps/tracing.py`). Note that until a `TracerProvider`/`MeterProvider`
is installed in the process, the OTel instruments resolve to the API's implicit
no-ops — recording stays cheap and correct, and the data flows out once a
provider is configured, with no change to handler code. So don't be surprised if
spans/metrics aren't exported yet; the wiring is provider-agnostic by design.

## Gotchas and conventions

- **Default to Go `streams`.** Only add a consumer here when it genuinely needs a
  Python-only library (NLP/ML). Most other use cases belong in `streams.go`.
- **Never block the event loop.** Wrap any synchronous CPU-bound call in
  `asyncify` / `anyio.to_thread.run_sync`. A blocking handler stalls _all_
  subscriptions on the same loop. The loop-lag histogram is your early warning.
- **Build expensive resources once**, in the handler's `__init__`, not per
  message.
- **Match ack/nack to intent.** Return to ack, raise to nack. For best-effort
  work with no DLQ, prefer acking (swallow `Exception`, log, return) so one bad
  message can't poison the subscription for the whole retention window. Always
  let cancellation (`BaseException`) propagate.
- **Don't leak content.** Security/PII handlers log entity types, counts, and
  ids — never scanned text, matched values, or raw error strings/tracebacks.
- **Declaration is a `gram-pubsub` task.** Adding a topic or subscription means
  editing a `.proto` and running `mise run gen:infra` — not editing `pystreams`.
  Use a _new_ subscription marker (e.g. a `Py*` variant) for a Python consumer of
  an already-consumed topic.
- **Build the image from the repo root**, not from `pystreams/` —
  `pystreams` is a uv workspace member that depends on `gram-infra`, and the
  single `uv.lock` lives at the root (see the header of `Dockerfile.pystreams`).
