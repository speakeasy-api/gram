# Clairic PII proof of concept

This opt-in adapter replaces the Presidio inference step with Cloudflare Workers
AI `@cf/cloudflare/clairic`. It is an experiment, not a validated replacement.
Presidio remains the default. Enabling Clairic sends scanned content to Cloudflare.
No infrastructure rollout or production credentials are included.

## Enable both paths

Supply `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_AUTH_TOKEN` through the local
ignored environment or a secret manager. The token needs Workers AI inference
permission. Do not commit these values.

1. Set `GRAM_PII_BACKEND=clairic` on **every pystreams role** (analysis and
   enforcement). The shared scanner factory then uses Clairic without loading
   spaCy models or starting a scan pool. Existing scan-worker flags apply only
   to Presidio; Clairic uses eight concurrent requests and a ten-second total
   deadline, including its queue wait.
2. Start the HTTP adapter from the repository root:
   `mise exec uv -- uv run --package pystreams clairic-server --port 8091`.
   It binds to loopback by default. For a container, explicitly set `--host
   0.0.0.0` and keep it on the private service network; this internal endpoint
   has no authentication and must not be publicly exposed.
3. Set `PRESIDIO_ANALYZER_URL=http://127.0.0.1:8091` on the Go server and worker
   (use the internal service address for containers), then restart those services.
   This covers inline background scans, legacy realtime enforcement, MCP policy
   scans, and the detection-rule playground. Setting only `GRAM_PII_BACKEND`
   does **not** switch these HTTP callers.
4. Exercise a synthetic text in the detection-rule playground and both Pub/Sub
   lanes. A successful scan produces existing `pii.<entity>` rules with source
   `presidio`. Errors continue through the existing retry/dead-letter and
   enforcement error paths. No adapter fallback silently substitutes Presidio.

To roll back, unset `GRAM_PII_BACKEND` (or set `presidio`), restore the original
`PRESIDIO_ANALYZER_URL`, and restart the same processes. The separate risk LLM
engine mode can bypass the Presidio lane; use its `off` mode when comparing
these two PII engines.

## Contract and limits

- One list-of-text field per selected entity; an empty entity list scans the
  full adapter catalogue. The catalogue covers every policy-catalogue entity
  plus legacy unpinned types. Unknown types fail explicitly.
- Lists preserve multiple occurrences. Every returned text must exactly match
  its supplied character span; malformed, missing or truncated answers fail
  the entire scan. No guessed positions or partial success.
- List confidence applies to every span in that list. This is not calibrated
  to Presidio's per-recognizer score; benchmark thresholds before enforcement.
- The Python path reuses the current exclusion catalogue and UTF-8 byte-offset
  conversion. The HTTP endpoint returns character offsets in the existing
  nested batch response; the Go client applies its own existing conversion and
  exclusions. Retired driver-license findings remain suppressed.
- Inputs above 256 KiB are rejected, not shortened. Model truncation also fails
  explicitly. Long-document chunking is outside this POC. Existing caller-side
  truncation and JSON transformation still apply before the adapter sees text.
- HTTP/network/API errors contain no scanned content or upstream response body.
  Cancellation propagates. Existing callers retain their fail-open/fail-closed
  decisions; this adapter does not change enforcement policy.
- `risk.clairic.input_tokens` and `risk.clairic.passes` count reported usage
  when the caller installs an OpenTelemetry meter provider (as pystreams does).
  The standalone compatibility server does not install an exporter. A benchmark
  harness must capture its own usage or install a meter provider. Missing usage
  and requests that fail before a usage response are **unknown cost**, not free.
  Existing product stoken billing is unchanged and is not provider cost.

## Validation and product surfaces

Transport tests use synthetic inputs and the documented REST envelope. They cover
Unicode, repeated spans, entity selection, thresholds, false-positive filtering,
invalid responses, deadlines, cancellation, startup configuration, and the HTTP
batch contract. They do not establish model accuracy or live API compatibility;
a credentialed smoke run is required before any rollout.

Platform MCP assessment: policy administrators continue to configure the same
risk rules and inspect the same redacted findings through existing risk tools.
This is an internal deployment choice, so no tool/schema/authorization change is
needed. The contract tests preserve entity IDs and finding shapes. No new demo
seed data or UI demo is needed: the existing PII policies and findings represent
the same product surface. No Temporal topology or actions are added.

Sources: [model and REST examples](https://developers.cloudflare.com/workers-ai/models/clairic/),
[extraction semantics](https://developers.cloudflare.com/workers-ai/concepts/extraction-models/).
