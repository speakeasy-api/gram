# LiteLLM 1.94.0 contract fixtures

These callback bodies were recorded from the exact image pinned in `compose.yml`:

```text
ghcr.io/berriai/litellm:v1.94.0@sha256:65d84a2282137b4dc73bbe184650a7c807177c533e4223b3bfbc87963fe3fabe
```

The recorder uses only a local stdlib HTTP fake provider and callback endpoint. It provisions ephemeral synthetic LiteLLM keys in an isolated temporary Postgres container. It never calls a real provider. IDs, key hashes, and inbound headers are normalized; optional and `null` callback fields are otherwise preserved.

## Required production configuration

`streaming_end_of_stream_only: true` is **REQUIRED**. Without it, LiteLLM's default streaming mode sends repeated cumulative response callbacks. Gram does not buffer or translate that mode. DNO-738 tracks production handling for the default behavior.

The canonical guardrail stanza is:

```yaml
guardrails:
  - guardrail_name: gram
    litellm_params:
      guardrail: generic_guardrail_api
      mode: [pre_call, post_call]
      api_base: https://example.test/rpc/litellm.ingest
      headers:
        Gram-Key: os.environ/GRAM_LITELLM_INGEST_KEY
        Gram-Project: os.environ/GRAM_PROJECT_SLUG
      default_on: true
      fail_on_error: true
      unreachable_fallback: fail_closed
      streaming_end_of_stream_only: true
      extra_headers:
        - x-gram-acting-principal
        - x-gram-acting-principal-contract
        - x-gram-inference-invocation-id
        - x-gram-session-id
        - x-claude-code-session-id
        - session-id
        - thread-id
        - x-session-id
```

The pinned LiteLLM 1.94.0 Generic Guardrail passes original request headers only when they are listed in `extra_headers`; otherwise `request_headers` contains presence placeholders. The three acting-principal headers are therefore required for the pre-inference checkpoint. Session headers remain attribution-only and never establish the actor.

Immediately before each inference attempt, the caller obtains `litellm-acting-principal.v1` from `POST /rpc/litellm.mintActingPrincipal` using an ordinary authenticated Gram user session plus the active managed instance ID and a fresh UUIDv7 invocation ID. The caller then sets the returned assertion, contract, and invocation ID as the three original request headers shown above. Existing values must be overwritten. A caller that has only a LiteLLM virtual key is not an authenticated actor and is not covered. The assertion is valid for 60 seconds and is bound to the organization, project, active managed instance, current integration API-key ID, invocation, issuer, audience, signing-key ID, token ID, and exact time window.

The `pre_call` callback is the only `ai_access` enforcement checkpoint. It runs before content extraction and provider inference for text, image, tool-only, and textless requests. `BLOCKED` is LiteLLM's acknowledgement that protected work must not continue; there is no separate acknowledgement or cached allow decision. `post_call` remains capture-only. Missing, invalid, stale, revoked, rotated, cross-tenant, or otherwise unavailable identity/resource state blocks with a safe unavailable message, and evaluator failures follow the registered fail-closed policy. Only a matched prescription returns its selected external note.

Customer-facing enforcement claims are limited to the exact LiteLLM 1.94.0 Generic Guardrail configuration above: `default_on: true`, `fail_on_error: true`, `unreachable_fallback: fail_closed`, and `streaming_end_of_stream_only: true`. Unmanaged instances, fail-open deployments, other LiteLLM versions, response blocking, and callbacks that do not preserve the original assertion headers are not claimed as covered. LiteLLM virtual-key user/email/end-user fields, integration API-key creator, session headers, and correlation caches are never acting-principal provenance.

## Coverage

- `openai-chat-tools.jsonl`: OpenAI Chat Completions, email-backed virtual key, tool definitions, historical tool calls, and output tool calls.
- `openai-responses-tools.jsonl`: OpenAI Responses, email-less virtual key, tool definitions, historical tool calls, and output tool calls.
- `anthropic-messages-tools.jsonl`: Anthropic Messages with the shared master key, Anthropic tool history, and output tool calls.
- `passthrough-text.jsonl`: LiteLLM generic pass-through field targeting and text-only fallback. LiteLLM OSS requires an enterprise license to grant a virtual key access to a configured pass-through route, so this isolated route is intentionally unauthenticated; identity variants are recorded on standard routes. LiteLLM omits forwarded request headers on this route, so Gram falls back to the trace ID for session identity. Version 1.94.0 also drops the logging context before configured pass-through post-call dispatch, so the pinned image emits only the request callback; pass-through response capture is not supported by this version.
- `streaming-chat.jsonl`: streaming request and the single end-of-stream cumulative response supported by Gram.
- `end-user-identity.jsonl`: caller-controlled end-user ID on an email-less virtual key.
- `shared-key-identity.jsonl`: virtual key with no bound user or email.

Every JSONL line is the callback body as received by the local recorder after the documented normalization. There are no synthetic default-stream callback files in this corpus; unsupported repeated callbacks are documented above rather than presented as recorded support.

## OTLP fixtures

`otlp-traces.json` and `otlp-traces.pb` are deterministic synthetic OTLP `ExportTraceServiceRequest` payloads shaped for LiteLLM 1.94.0 telemetry. They were not emitted or recorded from the LiteLLM image. The protobuf file is a direct protobuf encoding of the same pinned-version-compatible synthetic fields represented by the JSON fixture; manifest hashes keep both checked-in encodings stable. The payloads deliberately contain synthetic prompt, output, tool, header, metadata, and provider-payload attributes so privacy tests prove those fields are discarded.

`otlp-metrics.json` and `otlp-metrics.pb` similarly encode the six Histogram instruments emitted by LiteLLM 1.94.0 when metrics are enabled. The fixture includes cumulative temporality, buckets, units, and synthetic high-cardinality attributes so the metric allowlist and JSON/protobuf parity remain pinned.

## Real proxy end-to-end test

Docker must be running. Run the first-class local suite from the repository root:

```sh
mise run test:litellm-e2e
```

The suite starts the exact image in `manifest.json` and uses a synthetic local inference provider with real Gram auth, hooks, risk enforcement, capture, Redis idempotency, Postgres persistence, and durable risk analysis. It does not use external provider credentials. It verifies:

The test clones the canonical stanza with `default_on: false` only so each normal and outage posture can be selected independently; customer configuration keeps the documented `default_on: true`.

- Safe non-streaming requests return the fixture completion and capture exactly one user and one assistant message in the explicit session.
- Native Claude Code, Codex, and OpenCode session headers each correlate two turns into one ordered four-message conversation.
- A synthetic-secret policy violation returns the configured block message before a provider call, captures one blocked user message, and produces a durable gitleaks finding tied to that message.
- Safe streaming returns valid SSE and captures the user and assistant messages; a blocked stream is rejected before the provider and captures only the user message.
- A 503 guardrail outage fails closed before provider execution and captures nothing.
- The explicit `unreachable_fallback: fail_open` variant completes through the provider and captures nothing during the outage.
- A callback that persists the request and then exceeds the global timeout fails before provider execution; resending with the same `x-litellm-call-id` completes once and leaves exactly one user and one assistant message.

LiteLLM 1.94.0 does not retry a guardrail timeout. Deduplication applies when a client or gateway resends with the same call ID; an ordinary retry with a new call ID is a distinct call. Output blocking is not qualified by this suite. Streaming uses the required `streaming_end_of_stream_only: true`; DNO-738 tracks repeated default streaming callbacks.

This is not part of normal pull-request CI because the large third-party image introduces registry, network, and startup failure modes unrelated to most changes. The manual workflow runs the same mise task. To qualify a new LiteLLM version, update the fixture manifest through the sanctioned contract-fixture regeneration workflow, regenerate the fixture corpus, and require this suite to pass before documenting the version as supported.

## Regeneration

Docker must be running. From the repository root:

```sh
mise run gen:litellm-contract-fixtures
```

The task verifies the image digest and running image ID, records deterministic route traffic, rejects unsafe emails, credentials, authorization/cookie headers, and non-fixture organization IDs, then replaces generated JSONL files and writes the hash manifest last. CI verifies the complete manifest so interrupted or manually edited output fails the suite. CI consumes the checked-in corpus and does not run Docker generation.
