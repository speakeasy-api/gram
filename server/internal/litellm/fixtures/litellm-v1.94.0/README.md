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
      default_on: true
      streaming_end_of_stream_only: true
      extra_headers: [x-gram-session-id]
```

`extra_headers` is required to forward the session value rather than LiteLLM's `[present]` placeholder.

## Coverage

- `openai-chat-tools.jsonl`: OpenAI Chat Completions, email-backed virtual key, tool definitions, historical tool calls, and output tool calls.
- `openai-responses-tools.jsonl`: OpenAI Responses, email-less virtual key, tool definitions, historical tool calls, and output tool calls.
- `anthropic-messages-tools.jsonl`: Anthropic Messages with the shared master key, Anthropic tool history, and output tool calls.
- `passthrough-text.jsonl`: LiteLLM generic pass-through field targeting and text-only fallback. LiteLLM OSS requires an enterprise license to grant a virtual key access to a configured pass-through route, so this isolated route is intentionally unauthenticated; identity variants are recorded on standard routes. LiteLLM omits forwarded request headers on this route, so Gram falls back to the trace ID for session identity.
- `streaming-chat.jsonl`: streaming request and the single end-of-stream cumulative response supported by Gram.
- `end-user-identity.jsonl`: caller-controlled end-user ID on an email-less virtual key.
- `shared-key-identity.jsonl`: virtual key with no bound user or email.

Every JSONL line is the callback body as received by the local recorder after the documented normalization. There are no synthetic default-stream callback files in this corpus; unsupported repeated callbacks are documented above rather than presented as recorded support.

## Regeneration

Docker must be running. From the repository root:

```sh
mise run gen:litellm-contract-fixtures
```

The task verifies the image digest and running image ID, records deterministic route traffic, rejects unsafe emails, credentials, authorization/cookie headers, and non-fixture organization IDs, then atomically replaces the generated JSON/JSONL files. CI consumes the checked-in corpus and does not run Docker generation.
