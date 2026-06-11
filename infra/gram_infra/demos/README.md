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

2. From the `infra/` directory, run either demo:

   ```bash
   # synchronous (callback-based streaming pull)
   uv run pubsub-demo
   ```

You should see a message published every second and the subscriber log each one
as it is received. Press `Ctrl-C` to stop.

## What they show

- `pubsub_publisher_for_message(broker, ping_pb2.Message)` — a publisher for the
  topic declared by the `(gcp.pubsub.v1.topic)` option on `gram.ping.v1.Message`.
- `pubsub_subscriber_for_message(broker, ping_pb2.Message, processor_pb2.Processor)`
  — a subscriber for the `gram.ping.v1.Processor` subscription delivering
  `Message` payloads.
- Messages are protobuf-marshaled and tagged with `content-type` and `schema`
  attributes, so they interoperate with the Go publisher/subscriber.
