# presidio-scanner

An experiment that consumes `gram.risk.v1.PresidioRequest` Pub/Sub messages,
scans each `contents` entry with [Presidio](https://microsoft.github.io/presidio/),
and prints every detection to stdout as a logfmt `Finding` line.

It subscribes to the `gram.risk.v1.PresidioScanner` subscription declared in
`infra/proto/gram/risk/v1/scanners.proto` and reuses the `gram-infra` package
(sibling `infra/` project) for the generated protos and the type-safe Pub/Sub
subscriber.

## Setup (one-time)

```sh
cd presidio-scanner
uv sync
```

`uv sync` also provisions the `en_core_web_sm` spaCy model that Presidio's NLP
engine loads (it's pinned as a direct wheel dependency).

## Run

The local Pub/Sub emulator must be up (`docker compose up pubsub-emulator`, or
the usual dev stack). `PUBSUB_EMULATOR_HOST` is already set to `localhost:8088`
by the repo's `mise.toml`.

```sh
PUBSUB_EMULATOR_HOST=localhost:8088 uv run presidio-scanner
```

The broker reconciles the topic + subscription on demand, so no GCP resources
are needed.

## Feed it

Publish requests with the Go CLI (Stage 2):

```sh
cd infra
go run . presidio-submit "my email is alice@example.com and SSN 123-45-6789"
# scope detection to specific entity types:
go run . presidio-submit --entities EMAIL_ADDRESS "contact alice@example.com"
```

Each detection prints as:

```
rule_id=pii.email_address description="Email address" match=alice@example.com start_pos=12 end_pos=29 tags=pii source=presidio confidence=1 dead_letter_reason=
```

`start_pos`/`end_pos` are **byte** offsets (Presidio reports character offsets;
they're converted to match the Go `Finding`). If a content string can't be
analyzed after a few retries, a dead-letter sentinel `Finding`
(`rule_id=pii.dead_letter`, non-empty `dead_letter_reason`) is printed instead.
