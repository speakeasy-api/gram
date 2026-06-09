---
name: gram-pubsub
description: |
  A walkthrough of Gram's declarative GCP Pub/Sub system — topics and subscriptions are declared as options on protobuf messages, projected into Config Connector Helm values (`infra/gen/kcc.yaml`), and consumed at runtime through a type-safe publisher/subscriber library and the `gram streams` subscriber process. Activate this skill whenever the task involves Pub/Sub in Gram: adding or changing a topic or subscription, declaring a `(gcp.pubsub.v1.topic)` or `(gcp.pubsub.v1.subscription)` message option, regenerating `infra/gen/kcc.yaml`, publishing or receiving messages with the `infra/pkg/gcp` brokers, implementing a message handler and starting it up in the `gram streams` process, dead-letter queues, the local Pub/Sub emulator, or understanding why a declared topic has not yet appeared in a real GCP environment. It also applies to adjacent phrasing like "add an outbox topic", "create a subscription", "implement a subscriber", "wire up a message consumer", "consume messages off a topic", "register a stream handler", or "why isn't my topic being created in GCP" even when Pub/Sub is not named explicitly.
metadata:
  relevant_files:
    - "infra/proto/**/*.proto"
    - "infra/internal/gcp/*.go"
    - "infra/pkg/gcp/*.go"
    - "infra/cmd/infra/*.go"
    - "server/cmd/gram/streams.go"
    - "server/internal/streams/*.go"
    - "server/internal/ping/*.go"
    - "buf.yaml"
    - "buf.gen.yaml"
    - ".mise-tasks/gen/infra.sh"
---

# Gram Pub/Sub

Gram declares its GCP Pub/Sub topology **as data on protobuf messages**, not as
hand-written Terraform or Config Connector YAML. You annotate a "marker" message
with a topic or subscription option; a generator walks the compiled descriptors
and emits a Config Connector Helm values document (`infra/gen/kcc.yaml`); the
deployment tooling consumes that document to provision real topics and
subscriptions. The **same** proto options drive a type-safe Go
publisher/subscriber library, so the infrastructure and the application code can
never disagree about a topic's name or a subscription's wiring — both read from
one source of truth.

The whole point of the design is that an engineer adding a topic touches only a
`.proto` file under `infra/proto/`. Everything downstream — the Config Connector
specs, the per-environment rollout, and the runtime handle — is derived from it.
Everything in this skill lives in this repo (`infra/`); the deployment side is
referred to only abstractly as "the deployment tooling."

## Mental model: marker messages

A topic or a subscription is declared by attaching a **message option** to a
protobuf message. The message itself can be the event schema (for a topic) or an
empty placeholder (for a subscription) — what matters is the option, not the
fields. One message must not carry both a topic and a subscription option;
declare them separately. This keeps a topic's payload schema and a consumer's
config as distinct, independently evolvable things.

The option definitions live in `infra/proto/gcp/pubsub/v1/options.proto`:

- `(gcp.pubsub.v1.topic)` — `TopicOptions`: optional `name`, `retention_hint`,
  `labels`.
- `(gcp.pubsub.v1.subscription)` — `SubscriptionOptions`: optional `name`,
  required `topic` (the **proto full name** of the topic-declaring message, e.g.
  `"gram.outbox.v1.Event"`), plus `retention`, `ack_deadline`, `retry_policy`,
  `filter`, `dead_letter`, `expiration_ttl`, `retain_acked_messages`, `labels`.

## Authoring a topic

Add the topic option to the message that _is_ the event payload, so the schema
and the topic travel together. See `infra/proto/gram/outbox/v1/event.proto`:

```proto
message Event {
  string id = 1;
  string type = 2;
  google.protobuf.Timestamp created_at = 3;
  bytes payload = 4;

  option (gcp.pubsub.v1.topic) = {
    retention_hint: { seconds: 604800 /* 7 days */ }
  };
}
```

With no explicit `name`, the topic ID is the kebab-cased proto full name:
`gram.outbox.v1.Event` → `gram-outbox-v1-event`. Set `name` only when you need
to diverge from that.

## Authoring a subscription

Declare a subscription on its own marker message (no payload fields needed) and
point `topic` at the topic message's full name. See
`infra/proto/gram/outbox/v1/processor.proto`:

```proto
message Processor {
  option (gcp.pubsub.v1.subscription) = {
    topic: "gram.outbox.v1.Event"
    ack_deadline: { seconds: 30 }
    retry_policy: {
      minimum_backoff: { seconds: 10 }
      maximum_backoff: { seconds: 600 }
    }
    dead_letter: { max_delivery_attempts: 5 }
  };
}
```

With no explicit `name`, the subscription ID is the kebab-cased proto full name
(same rule as topics): `gram.outbox.v1.Processor` → `gram-outbox-v1-processor`.
Set `name` only when you need to diverge from that. The
`topic` reference is validated against the discovered topic set during
generation, so a typo fails the build rather than producing a dangling
subscription.

### Dead-letter queues are synthesized

When a subscription sets `dead_letter`, the generator **auto-creates a DLQ
topic** — you do not declare it. The default name is `<subscription>-dlq`
(override with `dead_letter.name`). The DLQ topic carries the same message
schema as the source and is labeled `dlq_for: <subscription>`. This is why
subscription IDs are length-capped below the topic limit: room must be left for
the `-dlq` suffix.

## Regenerating after a proto change

Always run the generator after editing any `.proto` under `infra/proto/` and
commit the result — `infra/gen/kcc.yaml` is the committed artifact the
deployment tooling consumes:

```
mise run gen:infra
```

This task (`.mise-tasks/gen/infra.sh`) does three things: `buf generate` (proto
→ Go in `infra/gen/`), `buf build` (compiled `FileDescriptorSet` →
`infra/cmd/infra/descriptors.pb`), then `go run ./infra/main.go gen-cc` to write
`infra/gen/kcc.yaml`. The generated Go and the descriptor blob are gitignored
(`**/descriptors.pb`) and excluded from formatting; `infra/gen/kcc.yaml` is
committed.

## How generation works (`infra/internal/gcp/`)

| File                 | Responsibility                                                                                                                                                                          |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pubsub_discover.go` | Walks the descriptor set, extracts options, resolves names, dedupes, validates, synthesizes DLQ topics. The `DesiredTopic` / `DesiredSubscription` structs are the in-memory topology.  |
| `cc_pubsub.go`       | Projects the topology into sorted Config Connector specs (`buildPubSubValues`).                                                                                                         |
| `values.go`          | The Helm values document types (`pubSubValuesDocument`). Specs embed the real Config Connector `PubSubTopicSpec` / `PubSubSubscriptionSpec` types so field names match the CRD exactly. |
| `cc.go`              | `ConfigConnectorPubSub.Provision` orchestrates discover → build → write, emitting the `# Code generated … DO NOT EDIT.` YAML.                                                           |

Key behaviors worth knowing:

- **Validation** (`validateTopicID` / `validateSubscriptionID`): GCP naming
  rules — must start with a letter, 3–255 chars, no `goog` prefix, no full
  resource paths. Duplicate topic or subscription names, an unknown `topic`
  reference, or a DLQ name colliding with a declared topic all fail generation.
- **Labels**: every generated resource gets `managed_by:
proto_pubsub_orchestrator` plus a `proto_message` label carrying the source
  message's full name; subscriptions also carry `topic_proto_message`. These
  make resources traceable back to their declaration.
- **Stable output**: topics and subscriptions are sorted by name so the
  generated file diffs cleanly across runs. Durations render as integer-second
  strings (e.g. `604800s`) as Config Connector expects.
- **Separation of concerns**: the generator emits _only_ the topology under a
  `pubsub` key (names, labels, specs). Per-resource deployment metadata
  (project, namespace, deletion/prune policy) is applied downstream by the
  deployment tooling, never here — so don't expect this generator to emit it.

## Runtime: publishing and subscribing (`infra/pkg/gcp/`)

Application code never hard-codes a topic or subscription name. It hands a proto
message to a **broker** and gets back a generic, type-safe handle. The broker
resolves names from the same proto options used for generation.

Two brokers implement both `PublisherBroker` and `SubscriberBroker`:

- `PubSubBroker` (`pubsub_gcp.go`) — talks to real GCP; assumes topics/subs
  already exist (Config Connector created them).
- `EmulatedPubSubBroker` (`pubsub_local.go`) — for local dev against the
  emulator; **reconciles** topics and subscriptions on demand since the emulator
  has no Config Connector.

Both take the embedded `descriptors` blob. Usage (condensed from
`infra/cmd/infra/demo.go`, the runnable reference):

```go
broker := gcppub.NewEmulatedPubSub(logger, projectID, client, descriptors)

// Publisher for the topic declared by *outboxv1.Event.
pub, _ := gcppub.PubSubPublisherForMessage(ctx, broker, &outboxv1.Event{})

// Subscriber for the *outboxv1.Processor subscription, receiving *outboxv1.Event.
// Read as: "a handle on the Processor subscription delivering Event messages."
sub, _ := gcppub.PubSubSubscriberForMessage(ctx, broker, &outboxv1.Event{}, &outboxv1.Processor{})

pub.Publish(ctx, &msg).Get(ctx)        // proto-marshaled, with content-type + schema attributes

// The callback receives the unmarshaled message plus delivery metadata.
// Return nil to ack; return a non-nil error to nack (and trigger redelivery /
// dead-lettering if enabled for the topic and subscription).
sub.Receive(ctx, func(ctx context.Context, msg *outboxv1.Event, meta gcppub.MessageMetadata) error {
    _ = msg            // already unmarshaled to *outboxv1.Event
    _ = meta.ID        // broker-assigned message ID
    _ = meta.Attributes
    _ = meta.DeliveryAttempt // set when dead-lettering is enabled, else nil
    return nil
})
```

`publisher.go` / `subscriber.go` define the generic `Publisher[M]` and
`Subscriber[M]` over `cloud.google.com/go/pubsub/v2`. Messages are
proto-marshaled and tagged with `content-type: application/x-protobuf` and a
`schema` attribute (the message full name); the subscriber unmarshals back into
a fresh `M` and hands it to your callback along with a `MessageMetadata`. The
callback's return value drives ack/nack — nil acks, non-nil nacks — so you no
longer call `Ack`/`Nack` yourself. Tune behavior with
`WithPubSubPublishSettings` / `WithPubSubReceiveSettings`.

This is the low-level library. **Inside the Gram server you almost never call
`sub.Receive` directly** — you write a `streams.Handler` and register it in the
`gram streams` process, which wraps the receive loop with tracing, panic
recovery, and ack/nack plumbing for you. See the next section.

## Implementing a subscriber in the server

Subscribers run in a dedicated long-running process, **`gram streams`**
(`server/cmd/gram/streams.go`). It is its own service — separate from the API
server and the Temporal worker — so message consumers scale and fail
independently. Locally it runs as the `streams` process under mprocs; start it
with `mise run start:streams`. Adding a consumer is two steps: write a handler,
then register it.

### Step 1 — write a handler

A consumer is a `streams.Handler[M]` (`server/internal/streams/handlers.go`),
where `M` is the proto message the topic carries:

```go
type Handler[M any] interface {
    Handle(context.Context, M, gcp.MessageMetadata) error
}
```

Implement it in the domain package that owns the behavior (not in `streams/` and
not in `cmd/gram/`). `server/internal/ping/handler.go` is the canonical
reference — a struct holding its dependencies, a `NewHandler` constructor, and a
`Handle` method:

```go
type Handler struct {
    logger *slog.Logger
}

func NewHandler(logger *slog.Logger) *Handler {
    return &Handler{logger: logger}
}

func (h *Handler) Handle(ctx context.Context, m *pingv1.Message, _ gcp.MessageMetadata) error {
    // ... do the work ...
    return nil
}
```

The handler's job is narrow: process one message and return. **The return value
is the ack/nack signal** — `nil` acks the message; a non-nil error nacks it,
triggering redelivery and eventual dead-lettering if the subscription declares a
`dead_letter` policy (see "Authoring a subscription"). So return an error only
when you genuinely want the message retried; for a poison message you can never
process, log it and return `nil` to drop it. Do not call `Ack`/`Nack`, start
your own receive goroutine, or open a span for the receive loop — the runner
does all of that (next step).

`HandlerFunc[M]` adapts a bare function to the interface when a struct is
overkill.

### Step 2 — register it in the streams runner

In `streams.go`, the `Action` builds the shared dependencies (db, redis, the
`gcp.NewPubSubBroker`) and a `receiverGroup`. Register each handler inside the
marked block with `mustReceive`:

```go
// Start subscription receivers in this block
{
    mustReceive(rg, &pingv1.Message{}, &pingv1.Processor{}, ping.NewHandler(logger))
}
```

The three positional arguments mirror the proto declarations:

- `&pingv1.Message{}` — the **topic** message; its type fixes `M`, the payload
  your handler receives.
- `&pingv1.Processor{}` — the **subscription** marker message (the one carrying
  `(gcp.pubsub.v1.subscription)`).
- `ping.NewHandler(...)` — your handler, with its dependencies injected from the
  ones the `Action` already constructed. If a handler needs the db or redis,
  pass them here; add new shared dependencies to the `Action` body.

Trailing `gcp.SubscriberOption`s (e.g. `WithPubSubReceiveSettings`) can follow.
Use `mustReceive` (it panics on a misconfigured subscription, which is a
programmer error caught at startup); `receive` returns the error if you need to
handle it.

### What the runner does for you

`receive`/`mustReceive` exist so handlers stay tiny. For each registration the
runner: resolves the subscription via `gcp.PubSubSubscriberForMessage`, launches
the receive loop in the shared `errgroup` (so any subscriber's fatal error tears
the whole process down for a clean restart), starts a `stream.handleMessage`
span per message, stamps pub/sub subscriber context values for telemetry,
**recovers panics** in your handler (converting them to a nacking error instead
of crashing the goroutine), and treats `context.Canceled` as a clean shutdown
rather than a failure. Because all of this is centralized, you get consistent
observability and failure semantics across every consumer without writing any of
it.

### Driving the flow end to end (optional publisher)

`ping` also ships a `StartPublisher` (`server/internal/ping/publisher.go`)
launched with `group.Go` alongside the receivers — a heartbeat that publishes a
message every few seconds so you can confirm the publish→subscribe loop is alive
in logs. It is a sanity-check harness, not a pattern to copy for real
publishers: production code publishes from wherever the event originates (an API
handler, a workflow activity) using `gcp.PubSubPublisherForMessage`, not from a
loop in the streams process.

## Local development

`mise.toml` sets `PUBSUB_EMULATOR_HOST` and `compose.yml` runs a
`pubsub-emulator` service (the `google/cloud-sdk` emulators image on port 8085).
With the emulator host set, `EmulatedPubSubBroker` creates topics/subscriptions
lazily on first publish/subscribe, so you don't need Config Connector locally.
The `infra demo` command (`go run ./infra/main.go demo`) is an end-to-end
publish/subscribe loop you can run to sanity-check the framework.

## How a declaration reaches a real environment

This is the part that most often confuses people, so it's worth being precise
about what this repo does and does not do.

- **Declaring a topic and committing `infra/gen/kcc.yaml` does not create
  anything in GCP.** The proto is the source; `infra/gen/kcc.yaml` is the
  committed artifact. Provisioning happens later, in separate deployment
  tooling, not as a side effect of merging.
- **Rollout is decoupled and version-pinned per environment.** Each environment
  runs the topology from a specific committed revision, not from `main` directly.
  A topology change therefore reaches one environment at a time as that
  environment is rolled forward — so a freshly declared topic legitimately may
  not exist yet in a given environment even though it is on `main`.
- **Local working ≠ deployed.** The emulator reconciles topics/subscriptions on
  the fly, so "works locally but missing in GCP" is the expected signature when
  the rollout simply hasn't reached that environment yet.
- **PR previews never create real topics**, by design.

So when something seems missing in a real environment, the productive question is
not "is the proto correct?" but "did `infra/gen/kcc.yaml` get regenerated and
committed, and has the environment in question been rolled forward to a revision
that includes it?" The rollout mechanics themselves live with the deployment
tooling, outside this repo.

## Gotchas and conventions

- **Run `mise run gen:infra` and commit `infra/gen/kcc.yaml`** after any proto
  change. A stale `kcc.yaml` is what actually ships — the proto is only the
  source. Regenerating without committing the result is the most common reason a
  declaration silently does nothing downstream.
- **`topic` references use the proto full name**, not the resolved topic ID
  (`gram.outbox.v1.Event`, not `gram-outbox-v1-event`).
- **Don't declare DLQ topics** — they're synthesized from `dead_letter`.
- **One option per message**: a message carrying both a topic and a subscription
  option fails generation by design.
- **Retiring a topic is a deliberate, decoupled act.** Removing a declaration
  stops the topology from managing it, but the deployment tooling is configured
  so existing GCP topics are not destroyed by a topology change. Treat removals
  as a separate, intentional step rather than assuming a deleted declaration
  tears down the resource.
