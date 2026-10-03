# Scan coverage for content past the truncation limit

Date: 2026-09-23

Decides AIS-726. Linked from AIS-717 (batch publish cap) and AIS-719 (chunked Presidio enforcement).

## Decision

Coverage is decided per scanner class, not per path, because the three scanner
families differ by three orders of magnitude in cost per byte. A single global
byte limit is the wrong instrument: at 50 KiB it throws away content the
deterministic scanners could read for free, and at the 1 MiB ceiling the same
flag already permits it is enough to take a Presidio analyzer worker down.

The limit is also not applied uniformly today, which is worth naming before
deciding where it should land. `risk-enforcement-max-content-bytes` truncates
only the Pub/Sub enforcement dispatch. In-process realtime gitleaks scans the
full text; in-process realtime Presidio is capped at 50 KiB by a separate
constant in `presidio.go`; the batch analysis topics apply no publish cap at
all, which is what AIS-717 set out to fix. "The truncation limit" is really
four different limits that happen to share a number.

**Class A — deterministic scanners scan the whole input.** Gitleaks and the
custom CEL rule analyzer scan all of the content available on the path, in
256 KiB chunks with 4 KiB of overlap, and stop being governed by
`risk-enforcement-max-content-bytes`. For the CEL analyzer this ratifies what
already happens: it runs in-process on the full message view and was never
truncated. For gitleaks it restores parity rather than extending reach — the
in-process realtime path already scans the full text untruncated, and only the
Pub/Sub enforcement lane cuts it to 50 KiB, so an organization's secret
coverage currently depends on which engine mode it is in. Chunking is not a
concession here: a chunked 10 MiB scan costs less CPU than an unchunked one
(1.45 s vs 6.27 s serial, 0.53 s across four cores), because gitleaks gates
each rule on a keyword pass over the whole fragment and one keyword in a large
input drags that rule's regex across all of it. Chunking also bounds the
in-process path, where a 10 MiB payload today occupies a blocking realtime
scan for 6 s.

**Class B — Presidio scans the whole input in chunks, up to an explicit
ceiling.** Chunk at 25 KiB with 1 KiB of overlap, dedup by absolute offset.
No single analyzer request may exceed 25 KiB — that bound is an availability
control, not a latency one. Ceilings past which the remainder is not scanned:
256 KiB realtime, 1 MiB batch. Realtime is additionally cut short by the
enforcement lane's existing wait budget, whichever binds first.

**Class C — model-backed judges keep a bounded window and are not chunked.**
The LLM analyzer, prompt-injection judge, and prompt policy judge keep the
existing 16k-rune head+tail window per body and per tool-call argument, and
the 50-tool-call cap. Chunking them would multiply model spend by the chunk
count and lower precision, because each chunk is judged without the
conversational context the verdict depends on. Head+tail also already
preserves the positions injected instructions favour.

**Payload ceilings per path.** Realtime dispatch inlines up to 1 MiB (the
existing `MaxContentBytes`); the default content limit rises from 50 KiB to
1 MiB for Class A while Class B keeps its own 256 KiB ceiling. Batch stops
inlining oversized content on the wire and publishes a blob reference for
content already in object storage, which the consumer fetches and chunks; the
hard ceiling is the 20 MiB `maxContentPartAssetReadSize` already applied when
hydrating content parts. Past a ceiling, content is not scanned.

**Partial coverage becomes durable and visible.** Every path records how much
of the input it read, on the finding and on the scan record, not only as a
metric. A partial scan that found nothing still emits a coverage marker, so
"we read the first 1 MiB of a 9 MiB attachment and found nothing" is
distinguishable from "we read all of it and it is clean". The ceilings are
stated in product docs.

This composes three of the four options the ticket listed — chunked consumer
scans, blob references for batch, and recording partial scans — and applies
the fourth, an explicit documented upper bound, as the backstop for Class B
and for the batch path. Truncation alone was rejected: for Class A it discards
coverage that costs nothing, and for Class B it is not even the cheaper option.

### Coverage table

| Path                        | Gitleaks / CEL rules                             | Presidio                                                    | LLM judges                |
| --------------------------- | ------------------------------------------------ | ----------------------------------------------------------- | ------------------------- |
| Realtime (hooks, inference) | Full content to 1 MiB, chunked                   | Chunked to 256 KiB, or until the lane's wait budget expires | 16k-rune head+tail window |
| Batch (Temporal, streams)   | Full content to 20 MiB, chunked, blob-referenced | Chunked to 1 MiB                                            | 16k-rune head+tail window |
| Per-request hard cap        | 256 KiB chunk                                    | 25 KiB chunk                                                | model context             |

Realtime tool _responses_ are out of scope of this decision because they are
not scanned in realtime at all today: `hooks/risk_scan.go` dispatches on user
prompts, before-tool-use, before-MCP-execution, and permission requests, whose
payloads are prompts and tool inputs. Large tool responses reach scanning only
through the batch path. That gap is a coverage question of its own, not a
truncation question, and needs its own ticket.

## Why, per class

### Gitleaks: full coverage is cheaper than the limit that hides it

Measured on the repository's own scanner (see `scansize_bench_test.go`), on a
4 vCPU Intel Xeon box:

| Input   | Single pass | 256 KiB chunks, serial | 256 KiB chunks, 4-way |
| ------- | ----------- | ---------------------- | --------------------- |
| 50 KiB  | 32 ms       | —                      | —                     |
| 256 KiB | 156 ms      | —                      | —                     |
| 1 MiB   | 621 ms      | 163 ms                 | 65 ms                 |
| 4 MiB   | 2.50 s      | —                      | —                     |
| 10 MiB  | 6.27 s      | 1.45 s                 | 0.53 s                |

Chunking wins by more than parallelism explains, and the mechanism matters
because it generalises. Gitleaks does one Aho-Corasick keyword pass per
fragment and only evaluates a rule's regex when one of that rule's keywords is
present anywhere in the fragment. Presence is per fragment, so a single
keyword-bearing line forces that rule across the entire input:

| 1 MiB input             | Single pass | 256 KiB chunks |
| ----------------------- | ----------- | -------------- |
| No keywords anywhere    | 174 ms      | —              |
| One secret-bearing line | 645 ms      | 143 ms         |

Chunking confines the regex to the chunk holding the keyword, so scanning
1 MiB that contains a secret costs less than scanning 1 MiB of inert text in
one pass. Raising gitleaks coverage from 50 KiB to 1 MiB therefore costs about
65 ms of wall clock on four cores, against a realtime lane budget of seconds.

Overlap is mandatory and cheap. A secret straddling a chunk boundary is missed
entirely by naive chunking and recovered by 4 KiB of overlap;
`TestChunkOverlapCoversBoundarySecret` pins that property so it survives the
implementation.

### Presidio: chunking is the only way to scan large content at all

Measured against `mcr.microsoft.com/presidio-analyzer:2.2.362`, the image the
local stack and the Go batch client both talk to, with PII-dense text:

| Input   | Latency  | Throughput |
| ------- | -------- | ---------- |
| 2 KiB   | 0.25 s   | 8 KiB/s    |
| 8 KiB   | 0.30 s   | 27 KiB/s   |
| 25 KiB  | 1.31 s   | 19 KiB/s   |
| 50 KiB  | 3.29 s   | 15 KiB/s   |
| 100 KiB | 10.27 s  | 10 KiB/s   |
| 150 KiB | 22.35 s  | 7 KiB/s    |
| 200 KiB | HTTP 500 | —          |

Latency grows faster than size — doubling 50 KiB to 100 KiB triples it — and
past roughly 150 KiB a single request stops completing. The 200 KiB failure is
not a rejection: the analyzer's gunicorn master logs `WORKER TIMEOUT` and kills
the worker, which takes out every other request that worker was serving. One
oversized scan is a multi-tenant availability event. This is the same symptom
the 50 KiB cap in `presidio.go` was sized against in May ("1 MB crashes the
analyzer"); the cliff is far lower than that comment implies.

Chunking is faster and loses nothing, provided it overlaps:

| 100 KiB total             | Wall clock | Recall vs single pass               |
| ------------------------- | ---------- | ----------------------------------- |
| One request               | 13.14 s    | baseline, 4409 spans                |
| 13 × 8 KiB, no overlap    | 3.53 s     | 99.61%, 17 spans lost on boundaries |
| 13 × 8 KiB, 256 B overlap | 3.65 s     | 100%                                |
| 4 × 25 KiB, 1 KiB overlap | 4.90 s     | 100%                                |

And it lifts the ceiling that a single request cannot cross: the 200 KiB input
that returns a 500 as one request completes as eight 25 KiB chunks in 9.51 s.

Chunked throughput lands near 28 KiB/s serial per analyzer, which is what sets
the Class B ceilings. 256 KiB of realtime content is about 9 s of serial
analyzer time, inside the lane's wait budget once chunks are spread over
replicas. 1 MiB of batch content is about 37 s, acceptable for an activity that
already heartbeats. 10 MiB would be roughly six minutes per message, which is
why Presidio does not inherit Class A's 20 MiB ceiling.

One implementation note: overlap produced a small number of extra spans (4415
against a 4409 baseline at 256 B overlap), so dedup has to merge overlapping
intervals rather than match exact offsets.

### Judges: the window is a control, not a shortfall

The 16k-rune head+tail window in `judgemessage/payload.go` is already
documented as a security control: an oversized payload that overflows the
judge's context window produces an error, which a fail-open policy converts
into an allow. Chunking replaces one bounded call with N calls, multiplying
spend linearly in content size, and each chunk is judged without the
surrounding conversation — the context an injection verdict actually turns on.
The window stays. What changes is that `body_truncated` stops being ephemeral
judge-payload state and reaches the finding.

## The caps are load-bearing and nothing says so

The measured curve is spaCy and recognizer cost, so it applies wherever
Presidio runs. Only the failure mode differs by deployment. The HTTP analyzer
the Go client talks to dies by gunicorn worker timeout. The realtime Python
enforcement path runs `AnalyzerEngine` in-process instead, so it fails by
exhausting its scan-slot budget and stalling the lane, and its 1 MiB
`MAX_CONTENT_BYTES` accept threshold sits above spaCy's default
1,000,000-character `nlp.max_length`.

Two independent 50 KiB constants currently keep both deployments below the
cliff, and neither is written down as an availability control — one is
`presidioMaxMessageBytes` in `presidio.go`, the other is the default behind
`risk-enforcement-max-content-bytes`. The flag in particular reads as a latency
knob, is clamped only by `MaxContentBytes` at 1 MiB, and can be set to that
today. Whoever raises either number without chunking in place gets a Presidio
outage rather than a slower scan. Capping a single Presidio request at 25 KiB,
in both deployments, is worth landing ahead of the rest of this decision.

## What this means for the linked tickets

AIS-717 should cap the batch publish at the transport bound and stamp the
message as partial rather than silently truncating, but it is an interim step:
the blob-reference work removes the reason the cap exists. It should not adopt
the 50 KiB realtime number, which has no meaning on the batch path.

AIS-719's chunking design generalises. The chunk-with-overlap, remap-offsets,
merge-spans engine is the same for gitleaks and Presidio and should be built
once in `scanners` rather than inside the Presidio enforcement handler.

## Follow-up tickets to file

1. Shared chunked-scan engine in `server/internal/scanners`: chunk with
   overlap, remap chunk-relative offsets to absolute, merge overlapping spans,
   report bytes scanned. Extends AIS-719.
2. Cap a single Presidio analyzer request at 25 KiB across the Go batch client
   and the Python handler. Availability fix, ship first.
3. Presidio chunked scanning on both paths, to the 256 KiB realtime and 1 MiB
   batch ceilings.
4. Gitleaks and CEL rules on full content via the shared engine; raise the
   realtime default content limit to 1 MiB for those lanes and stop applying
   the global truncation to them.
5. Coverage fields — `scan_coverage`, `scanned_bytes`, `content_bytes` — on the
   `Finding` proto, ClickHouse `risk_findings`, and Postgres `risk_results`,
   including a coverage marker for a partial scan with no findings.
6. Surface partial coverage in Risk Events and the finding detail view.
7. Blob-reference publishing for oversized batch analysis payloads, with
   consumer-side fetch, replacing inline content on the analysis topics.
8. Product docs stating the per-path, per-scanner ceilings.
9. Separate coverage question, not truncation: realtime scanning of tool
   responses, which no hook path scans today.

## Method

Gitleaks numbers come from `BenchmarkScanBySize`, `BenchmarkScanKeywordDense`,
and `BenchmarkScanChunked` in
`server/internal/scanners/gitleaks/scansize_bench_test.go`, run with
`go test ./internal/scanners/gitleaks/ -run '^$' -bench BenchmarkScan
-benchtime 25x`, Go 1.27.1, linux/amd64, 4 vCPU Intel Xeon. The keyword
locality rows and the finding-count sweep were one-off variants of the same
harness. Content is synthetic: JSON-ish log lines for the keyword-free case,
`api_key`/`token`/`secret` assignments for the dense case, one GitHub PAT for
the finding case. Single-run observations on a shared box, so treat ratios as
the result and absolute values as indicative.

Presidio numbers come from the local shared analyzer container,
`mcr.microsoft.com/presidio-analyzer:2.2.362` on 127.0.0.1:5050, driven by a
throwaway Python client posting `{"text": [...], "language": "en",
"score_threshold": 0.5}` to `/analyze` and timing the round trip. Content is
repeated PII-dense support-ticket prose carrying a name, email, phone, credit
card, IP, date, and location per line, so recognizer load is high and finding
counts are large enough for a recall comparison to mean something. Recall is
measured as distinct `(entity_type, absolute_start, absolute_end)` spans
recovered against the single-pass result for the same text. The container is
the dev stack's analyzer with default gunicorn settings and shares the box with
everything else, so the absolute latencies are pessimistic against production
capacity; the shape of the curve and the failure threshold are the findings
that carry.
