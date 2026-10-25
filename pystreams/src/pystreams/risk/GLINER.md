# GLiNER2 PII backend

This replaces the Clairic POC with local GLiNER2 PII. The priority is **near-zero
false positives first, recall of at least 80% second**. Presidio remains the
production default until held-out validation meets those requirements. Selecting
GLiNER replaces the scanner; it does not silently fall back to Presidio on errors.

## Run locally

From the repository root:

```sh
uv sync --package pystreams --extra gliner
uv run --package pystreams --extra gliner python -c 'from huggingface_hub import snapshot_download; from pystreams.risk.gliner import MODEL_ID, MODEL_REVISION; snapshot_download(MODEL_ID, revision=MODEL_REVISION)'
GRAM_PII_BACKEND=gliner uv run --package pystreams --extra gliner multi
```

The checkpoint is `fastino/gliner2-privacy-filter-PII-multi`, pinned to
`1cb4166094dc58fa8d836429f060d6c95f62b495`. Serving loads only that cached
revision, on CPU in FP32, with four PyTorch threads. No Cloudflare credentials,
API calls, or runtime model downloads are required. Missing dependencies or
weights fail startup. Provision the cache using the same `HF_HOME` as the service.

For the existing Go inline, MCP, and playground scan paths:

```sh
uv run --package pystreams --extra gliner gliner-server --port 8091
```

Point `PRESIDIO_ANALYZER_URL` at `http://127.0.0.1:8091/analyze`. The endpoint
accepts the existing batch-of-texts request and returns character spans; Go keeps
its existing filters and UTF-8 conversion. The Python consumers reuse the same
finding conversion and exclusions. Keep the endpoint private; it has no public
authentication layer. Health becomes available only after model load completes.

Build an image with dependencies and the checkpoint baked in:

```sh
docker build --build-arg WITH_GLINER=1 -f pystreams/Dockerfile.pystreams -t pystreams-gliner .
```

Set `GRAM_PII_BACKEND=gliner` when running that image. The default image build
omits the optional model dependencies and weights. The HTTP adapter can run from
the same opt-in image with its entrypoint set to `gliner-server`; bind `--host
0.0.0.0` only on the private service network. Image build validation and deployment
capacity sizing are still required before rollout.

## Precision policy

`GRAM_GLINER_MIN_SCORE` defaults to **0.9**. The effective threshold is the larger
of this floor and the caller's `score_threshold`, so an existing policy's 0.5
cannot accidentally lower the floor. The environment setting accepts a finite
number in [0, 1]. This is a candidate operating point, not a calibrated guarantee.
Confidence values are not interchangeable with Presidio scores.

Re-scoring the existing 36-document synthetic run gives:

| Minimum score | True positives / 384 | False-positive spans | Recall |
| ------------- | -------------------: | -------------------: | -----: |
| 0.5           |                  384 |                   19 | 100.0% |
| 0.8           |                  379 |                    6 |  98.7% |
| 0.9           |                  340 |                    4 |  88.5% |
| 0.95          |                  301 |                    3 |  78.4% |
| 0.999         |                  152 |                    0 |  39.6% |

A full replay through this adapter on the repository’s Python 3.14.8 runtime
confirmed 340 true positives and four false-positive spans across all 36 documents
at the default floor. At 0.9, observed precision is 98.8%. **This does not yet meet a near-zero
false-positive requirement.** These are before-exclusion, exact typed-span scores
on the same exploratory corpus, not held-out calibration. The corpus has only
eight distinct synthetic values and twelve clean documents. Its zero clean-document
false alarms does not establish a production false-alarm rate.

Validation must choose a threshold on a separate calibration set, then freeze it
before evaluation. Optimize false positives first; do not lower thresholds merely
to improve recall. Proposed numerical release gates are at least 99.9% precision,
at most 0.1% clean-document false alarms, and at least 80% recall, reporting 95%
confidence intervals and results per enabled entity type. Use thousands of
independent hard-negative documents, varied values, languages, and document sizes.
If no operating point passes both priorities, keep the current production backend
and investigate type-specific thresholds or validators on calibration data.

## Coverage and runtime limits

The benchmarked mappings are person, email, phone, payment card, IBAN, US SSN, IP,
and NHS number. Native model labels are used where available; NHS is an additional
zero-shot label. The remaining policy-catalogue and legacy entity types have
explicit descriptive zero-shot labels so policy requests are not silently dropped.
**They have not been accuracy-validated**, especially medical entities, regional
identifiers, and legacy unscoped scans. They must pass their own gates before use.
Unknown entity IDs are rejected. No policy catalogue, permissions, or finding
schema changes are part of this PR.

The model loads once per process. Inference runs off the event loop and is
serialized through one slot; process-pool worker settings do not create extra
model copies. The existing `scan_timeout` and `scan_slot_timeout` apply in the
Python factory. The standalone adapter uses 30 seconds and one second respectively.
The model call has a **soft** deadline: CPU inference cannot be safely interrupted,
so an over-budget call fails after the worker finishes. Cancellation retains the
slot until that worker finishes, preventing detached inference from accumulating.
Scale through separate processes/replicas after measuring memory and throughput.

Long-document extraction uses the official helper with 256 model-word-token
chunks, 48-token overlap, and batch size one. It merges overlapping predictions
back into document coordinates. This scans the whole input instead of truncating
a prefix; a single entity still must fit inside one chunk. Inputs above 256 KiB
are rejected, not truncated. Invalid/missing entity output, invalid confidence,
or source-offset mismatch fails the whole scan. Duplicate exact spans are merged.
The HTTP adapter limits batches to 32 texts and request bodies to 2 MiB, and returns
sanitized scan errors rather than an empty success. No scanned content is logged.

## Benchmark and rollout

[Long-document results and methodology](https://github.com/speakeasy-api/gram/pull/8063#issuecomment-6401881089)
compare all five engines. At threshold 0.5, GLiNER2 PII scored 97.6% F1 before
exclusions, with a 4.43-second median for roughly 32 KiB on an Apple M5 Pro CPU.
Peak process RSS was 2.96 GiB. Those are exploratory single-pass measurements,
not container sizing or service-level guarantees. Local inference removes API
charges, but compute, memory, idle capacity, and operations still have a cost.

Before rollout: run the held-out precision-first evaluation above through the
actual adapter, validate the image on the deployment architecture, load-test
queue saturation and soft deadlines, and size replicas from measured throughput.
Use shadow traffic before enforcement. Roll back Python with
`GRAM_PII_BACKEND=presidio` and Go by restoring `PRESIDIO_ANALYZER_URL` to the
existing Presidio service. No production deployment is included here.

Platform MCP assessment: this is an internal implementation switch for existing
PII policy outcomes, with unchanged actors, authorization, schema, and tool
contracts; no new or modified Platform MCP tool is needed. There is no new
user-visible data surface, so demo seed and UI capture changes are not needed.
