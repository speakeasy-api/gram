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

## Accuracy and cost benchmark plan

The decision is whether Clairic can replace Presidio at equal or better detection
quality and acceptable enforcement latency. Run the experiment offline before
considering a shadow deployment. The numbers below are proposed acceptance gates,
not measured results.

### 1. Prove the live contract

Use a dedicated Workers AI token with a small spend cap. Run 50 synthetic cases
through the direct scanner and HTTP adapter: empty results, multiple entities,
repeated values, Unicode (including emoji and combining characters), escaped JSON,
long inputs, and malformed/over-limit responses. Confirm the actual REST envelope,
list confidence, character-offset convention, truncation flag, and usage fields.
Record model identifier, run date, adapter commit, and response schema. Stop on a
contract mismatch; do not reinterpret invalid spans as clean results.

### 2. Freeze an annotated evaluation set

Start with 10,000 documents: 7,000 positive/mixed examples and 3,000 hard negatives,
with at least 200 annotated occurrences per active entity. Cover chat text, tool
arguments/results, source code, logs, prose, structured JSON, and common payload
sizes. Include international identifiers, repeated PII, obfuscation, Unicode,
reserved example data, UUIDs/version strings, contextual healthcare references,
and instructions that tell the scanner to ignore PII. Separate all-entity policies
from the entity subsets used by existing policies.

Use generated fictional identities and licensed public evaluation data. Keep any
later approved internal cohort private and outside Git; do not send existing
customer content to a new provider as part of this initial benchmark. Two reviewers
label a representative sample and resolve disagreements. Label entity type plus
exact UTF-8 span and distinguish deliberate exclusions from detector misses.

Split 20% calibration / 80% held-out evaluation by template family and source,
not random near-duplicate rows. Freeze labels before model runs. The harness must
save corpus/version hashes, field instructions, selected entities, thresholds,
engine versions, outcomes, timing, and usage. A future harness can live under
`pystreams/src/pystreams/cmd/`; its implementation is a follow-up to this POC.

### 3. Compare quality at matched operating points

Run the pinned Presidio baseline and Clairic on identical texts. Evaluate both raw
extraction and final findings after the same exclusions and caller preprocessing.
Measure per-entity and micro/macro precision, recall, and F1; exact-span accuracy;
character coverage; document-level false blocks; and missed sensitive documents.
Show unsupported classes and failures separately. Failed/truncated requests count
against completion and coverage rather than disappearing from the denominator.

Sweep thresholds on calibration data only, then freeze them. Compare the engines
at matched recall and at matched false-block rate; their confidence scales are not
interchangeable. Use paired document-level bootstrap intervals (95%) for differences.
Report sparse classes as inconclusive instead of passing them on a high point score.
Repeat 10% of the held-out set three times to measure model instability.

### 4. Measure latency and fully loaded cost

Replay the same size/entity mix at concurrency 1, 4, 8, 16, and 32. Measure cold and
warm runs, p50/p95/p99 end-to-end latency, queue time, throughput, timeout rate,
429s, retries, and successful scans. Exercise both direct Pub/Sub and HTTP routes;
include existing broker/Redis work when measuring enforcement deadlines.

Capture provider-reported `input_tokens`, `schema_tokens`, and `passes` per attempt,
including retried or rejected results. Do not estimate cost from content length
alone or multiply `input_tokens` by `passes` again unless billing semantics require
it. Missing usage remains unknown and must reconcile with provider billing.
As of 2026-10-25, the model page lists **$0.10 per million input tokens**; recheck
that rate on the run date. At that rate, 2,000 billed input tokens per scan would
cost $200 per million scans before retries and hosting. This is an illustration,
not a workload estimate.

Compare against measured Presidio CPU-seconds, memory, replicas, idle capacity,
and hosting/network cost at the same throughput and availability target. Include
the Python adapter's own hosting cost; this POC still ships Presidio dependencies.
Report cost per 1,000 successful scans, per million input characters, and per true
positive at matched recall. Show monthly estimates for the actual private traffic
mix with low/base/high volume, not public customer spend figures. Keep existing
product stoken billing separate from this provider-cost calculation.

### 5. Make the rollout decision

Proposed gates: no observed invalid accepted spans or silent truncation; completion
at least 99.9%; critical-ID recall at least 99%; and no more than a one-percentage-
point recall regression against Presidio (using the paired confidence interval).
The clean-document false-block rate must be no worse than Presidio and below 0.5%.
Expand the corpus when confidence intervals are too wide to establish a gate.

Require p99 end-to-end time to fit the deployed enforcement deadline with 20%
headroom. For a cost-led replacement, target at least 20% lower fully loaded cost
at matched recall. A quality improvement with higher cost needs an explicit product
tradeoff. Publish a per-entity scorecard, error samples with fictional values,
latency curves, a reconciled cost table, and a go/no-go recommendation.

Only after these gates pass, propose a bounded shadow rollout with independent
provider attribution and a kill switch. Do not change the default or mix unlabelled
Clairic results into the Presidio baseline during the experiment.

Unresolved before execution: Cloudflare test credentials and spend cap; the target
enforcement deadline/traffic mix; agreement on the proposed quality and cost gates.
