# Pub/Sub demos

Runnable demos for the `gram_infra.pubsub` convenience layer — the Python
counterpart of the Go `infra demo` command (`infra/cmd/infra/demo.go`). They are
exposed as console scripts (see `[project.scripts]` in `pyproject.toml`).

## Run

Both use the `EmulatedPubSubBroker`, which reconciles the topic, subscription,
and dead-letter topic on demand against the local Pub/Sub emulator — no GCP
resources or Config Connector required.

1. Start the local Pub/Sub emulator (runs as part of the local stack via
   `madprocs` / `compose.yml`, on the `PUBSUB_EMULATOR_HOST` port — by default
   `localhost:8088`, see `mise.toml`).

2. From the `infra/` directory, run a demo. Two are provided — they differ only
   in how they consume messages:
   - `pubsub-demo` — the **callback** API (`subscriber.receive`), with two
     competing subscribers.
   - `pubsub-stream-demo` — the **async-iterator** API (`subscriber.stream`),
     consuming a single subscription with an `async for` loop and acking each
     message explicitly.

   ```bash
   # asyncio backend (default)
   uv run pubsub-demo
   uv run pubsub-stream-demo

   # trio backend — the same demos, proving the library is backend-agnostic
   PUBSUB_DEMO_BACKEND=trio uv run pubsub-demo
   PUBSUB_DEMO_BACKEND=trio uv run pubsub-stream-demo
   ```

You should see a message published every ~200ms and the subscriber log each one
as it is received. The first log line reports the active backend. Press `Ctrl-C`
to stop.

The publisher/subscriber layer is built on [anyio](https://anyio.readthedocs.io),
so it runs unchanged on either backend; `PUBSUB_DEMO_BACKEND` just selects which
one `anyio.run` uses (trio is a dev dependency via `anyio[trio]`).

## What they show

- `pubsub_publisher_for_message(broker, ping_pb2.Message)` — a publisher for the
  topic declared by the `(gcp.pubsub.v1.topic)` option on `gram.ping.v2.Message`.
- `pubsub_subscriber_for_message(broker, ping_pb2.Message, processor_pb2.Processor)`
  — a subscriber for the `gram.ping.v2.Processor` subscription delivering
  `Message` payloads.
- Messages are protobuf-marshaled and tagged with `content-type` and `schema`
  attributes, so they interoperate with the Go publisher/subscriber.
