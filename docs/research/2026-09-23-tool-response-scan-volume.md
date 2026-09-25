# Tool-response volume in Presidio risk scanning

Date: 2026-09-23
Ticket: AIS-723

## Result

Before any content-reduction work is worth doing, two structural facts about the scan path have to be accounted for, because both of them shape the numbers the ticket starts from.

**Every batch-scanned message is scanned by Presidio twice, on two lanes that prepare the content differently, and each lane writes its own meter reading.** `scanStandardPolicy` publishes a `PresidioAnalysis` request for the message and then scans the same message inline in the same goroutine ([`batch_scanners.go:241-261`](../../server/internal/background/activities/risk_analysis/batch_scanners.go)). The inline lane reformats the payload from JSON to YAML, caps it at 50 KiB, and counts stokens over that prepared text. The async lane publishes the raw stored content with no reformat and no cap, and the pystreams consumer scans all of it. The two readings differ only in their `scan_execution_path` attribute — `inline_batch` and `shadow_stream` — and because the execution path is part of the operation id, they get distinct deterministic reading ids and both land in the ledger. Any `gram.risk.scan.presidio` total that does not group by `scan_execution_path` is adding two scans of one message together. Query 1 in the accompanying SQL settles the production split in one line, and it is the first thing to run.

**On the ledger, the oversized tail is entirely an async-lane phenomenon.** `analyzeOne` returns `Completed: countErr == nil && !truncated`, and `recordBatchResults` skips any result that is not completed, so a tool response over 50 KiB is scanned inline (truncated) and metered at zero. The only lane that records stokens for a 500 KiB tool response is `shadow_stream`, which records all of them. The ticket's observation that roughly half the volume comes from the 1 to 2% of responses over 50 KiB is therefore, by construction, a statement about the uncapped async lane. It also means cost and coverage currently sit on opposite lanes: the lane that pays nothing for large responses is the one that stops reading at 50 KiB, and the lane that reads the whole thing pays for every token of it.

With that established, the content findings:

- **The per-tool question the ticket asks cannot be answered from the ledger at all.** `tool_name` is empty on every `tool_response` reading. `batchRiskProvenance` fills it only from the scanned message's own `tool_calls` array ([`batch_scanners.go:94-99`](../../server/internal/background/activities/risk_analysis/batch_scanners.go)), and a tool-result row has none — `newBatchMessage` populates `ToolCalls` only when the type is `tool_request`. `GetMessageContentBatch` does not even select `tool_call_id`, so the batch scanner has no way to recover the name. Per-tool cost has to come from `gram.telemetry_logs`, which does carry a materialized `tool_name`, until that attribution is fixed.
- **The single largest piece of removable content is Claude Code's edit family.** An `Edit` response carries `originalFile`, and `MultiEdit` carries `originalFileContents`: the entire pre-edit file, verbatim, re-sent on every edit. On a corpus of this repository's own source files, that field is 91.5% of an `Edit` response's scanned tokens and 77.1% of a `MultiEdit`'s. Gram forwards and stores it untouched — the relay passes `tool_response` through as `tool_call.output` and the server writes `json.Marshal(output)` straight into `chat_messages.content`, with no size limit anywhere on that path.
- **Content hashing pays off only at field granularity.** Hashing whole messages saves 1.9% of a simulated editing session, because consecutive `Edit` responses differ in their `oldString`, `newString`, and patch even when the file they restate is identical. Hashing the echoed file field and scanning each distinct file once per chat saves 71.9% of the same session.
- **Of the sources that run on tool responses, only the judge needs the response as one piece.** `shadow_mcp`, `destructive_tool`, and `cli_destructive` are scoped to `kind == "tool_request"` in the recommended-scope registry and never read a tool response. That leaves gitleaks, Presidio, and prompt injection. Gitleaks and Presidio produce span-local matches over a text; they need to see all the bytes but not in one request, so windowing preserves recall at the same token cost, while the current truncation simply drops coverage past 50 KiB. The prompt-injection judge is the one source whose verdict depends on reading the response as a whole, and it is also the one that cannot be pointed at a megabyte of file content in the first place.

## What runs today

For a stored tool response, the path is: hook relay → `/rpc/hooks.ingest` → `chat_messages` row with `role = 'tool'` → risk coordinator signal → `AnalyzeBatch`. Inside `AnalyzeBatch`, the Presidio source fans out as follows.

|                      | `inline_batch`                                                                   | `shadow_stream`                                                            |
| -------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| Engine               | Go `PresidioClient` over HTTP `/analyze`                                         | pystreams `PresidioHandler`, spaCy + Presidio in process                   |
| Scanned text         | `PrepareScanText`: JSON reformatted as YAML, capped at 50 KiB on a rune boundary | `msg.scanSurface()` verbatim, uncapped                                     |
| Stokens counted over | the prepared, capped text                                                        | the raw text                                                               |
| Meter written by     | the Go worker, via `recordBatchResults`                                          | the Python consumer, from a template the Go worker embedded in the request |
| Oversized message    | scanned truncated, `Completed = false`, **not metered**                          | scanned in full, metered in full                                           |
| Findings land in     | Postgres `risk_results`                                                          | ClickHouse `risk_findings`                                                 |

The same publish-then-scan-inline structure exists for gitleaks (`scan_gitleaks.go`) and prompt injection (`scan_prompt_injection.go`), both of which also publish `msg.scanSurface()` uncapped. Whether their async lanes also meter is worth confirming with query 1 retargeted at `gram.risk.scan.gitleaks` and `gram.risk.scan.prompt_injection` before assuming this is a Presidio-only problem.

Two more properties matter for the options below.

Detection scopes cannot see a tool name on a tool response. The CEL environment exposes `kind`, `content`, `prompt`, `assistant`, `tool_result`, and `tool_calls`, and `batchMessageView` populates `Tools` only for `tool_request`. A recommended scope such as "exempt `Read` output from PII scanning" is not expressible today. The registry already notes a `kind == "tool_response"` exemption for the off-policy category as a candidate pending corpus validation, which is the all-or-nothing version of the same idea.

Telemetry is a usable measurement surface. Hook ingest writes the tool result to `gen_ai.tool.call.result` directly, without the 64 KiB `truncateBody` cap that the HTTP and MCP gateway paths apply, so lengths read out of `gram.telemetry_logs` for `PostToolUse` rows are the real stored sizes.

## Measurements

Numbers below come from `server/cmd/tools/scanvolume`, which runs the production `PrepareScanText` and the o200k_base stokens codec over tool-response payloads reconstructed field for field from real source files. It measures the preparation and counting code the scanner actually uses, so the token arithmetic is exact; the payload shapes are reconstructed from Claude Code's observable hook output rather than read out of production, so treat the shape mix and absolute totals as illustrative and the per-field ratios as the load-bearing part.

```
cd server && go run ./cmd/tools/scanvolume -root . -files 800 -edits 8
```

800 files, 15,983,731 bytes of source text. One response of every shape per file.

| Shape     |   n | p50 KiB | p90 KiB | max KiB | >50 KiB | raw stokens | normalized |    inline |   metered | ledger total |
| --------- | --: | ------: | ------: | ------: | ------: | ----------: | ---------: | --------: | --------: | -----------: |
| Read      | 800 |     5.2 |    29.5 |  1523.8 |    6.4% |   4,834,751 |  4,355,913 | 2,180,383 | 1,537,395 |    6,372,146 |
| Edit      | 800 |     6.2 |    30.6 |  1524.6 |    6.8% |   5,146,637 |  4,693,269 | 2,433,300 | 1,750,589 |    6,897,226 |
| MultiEdit | 800 |     9.1 |    34.1 |  1528.7 |    7.4% |   5,849,596 |  5,466,269 | 3,077,020 | 2,342,587 |    8,192,183 |
| Write     | 800 |     5.7 |    30.1 |  1524.2 |    6.5% |   4,992,390 |  4,536,113 | 2,335,077 | 1,679,336 |    6,671,726 |
| Grep      | 800 |     1.5 |     6.3 |   614.5 |    0.1% |     784,563 |    743,757 |   553,246 |   537,875 |    1,322,438 |
| Bash      | 800 |     5.1 |    29.4 |  1523.7 |    6.4% |   4,816,656 |  4,333,683 | 2,184,417 | 1,541,795 |    6,358,451 |
| MCP       | 800 |     5.1 |    29.4 |  1523.7 |    6.4% |   4,817,456 |  4,378,207 | 2,160,676 | 1,505,638 |    6,323,094 |

`raw` is what the async lane reads and meters, `normalized` is the same content after the JSON-to-YAML reformat without the cap, `inline` is what the Go client posts to `/analyze`, and `metered` is the `inline_batch` ledger value. Across the corpus the two lanes read 46,166,168 stokens and the ledger records 91.3% of that, the gap being the oversized messages the inline lane scans without metering. The reformat the async lane skips is worth a 1.10x multiplier on identical content.

Cost of the field that only restates a file the pipeline already read:

| Shape     | scanned stokens today | without `originalFile` | change |
| --------- | --------------------: | ---------------------: | -----: |
| Edit      |             7,579,937 |                640,586 | -91.5% |
| MultiEdit |             8,926,616 |              2,043,052 | -77.1% |

Each lever on its own, measured as scanned stokens rather than ledger stokens so the accounting defect does not contaminate the scanner arithmetic:

| Option                                                        | scanned stokens | change |
| ------------------------------------------------------------- | --------------: | -----: |
| today                                                         |      46,166,168 |  +0.0% |
| normalize the async lane, no cap change                       |      43,431,330 |  -5.9% |
| cap both lanes at 50 KiB                                      |      29,848,238 | -35.3% |
| one lane only, async as it is today                           |      31,242,049 | -32.3% |
| one lane only, normalized and capped                          |      14,924,119 | -67.7% |
| drop `originalFile` from the edit family, both lanes as today |      32,343,253 | -29.9% |
| one normalized capped lane and no `originalFile`              |      10,710,516 | -76.8% |

Repeated content, modelled as one chat per file containing a `Read` and then eight `Edit`s of that file, 7,200 messages in total:

| Strategy                                              | scanned stokens | change |
| ----------------------------------------------------- | --------------: | -----: |
| today                                                 |      68,679,754 |        |
| skip messages whose whole content was already scanned |      67,368,178 |  -1.9% |
| scan each distinct `originalFile` once per chat       |      19,322,162 | -71.9% |
| drop `originalFile` outright                          |      13,164,648 | -80.8% |

The field-dedup row is conservative: it charges the first `Edit` for the file body even though the preceding `Read` already carried the same bytes inside its own envelope. A hash keyed on file content rather than on the response field would also collapse that pair.

## Ranked options

Ordered by saving per unit of work, with what each one costs in detection.

**1. Stop scanning every message twice.** The two lanes run the same detector over the same message and disagree only on preparation. Consolidating on one — keeping the async lane as the engine and moving the Postgres `risk_results` write behind it, or retiring the async lane and keeping inline — removes a whole scan and a whole meter reading per message. Worth between a third and two thirds of scanner volume depending on which lane survives, and it costs no detection at all, because the surviving lane sees the same content. This is the largest single lever and the one that needs the most care: the lanes write to different stores, and the async lane is the only one producing ClickHouse findings.

**2. Extract the edit family's scan surface.** Drop `originalFile` and `originalFileContents` before scanning, keeping `oldString`, `newString`, and `structuredPatch`. Removes roughly 90% of an `Edit` response's tokens. The detection cost is real but narrow: PII or a secret that sits in an untouched region of an edited file stops being re-detected on every subsequent edit of that file. It is still detected when the file is read, written, grepped, or `cat`-ed, and when the edit touches it. Best implemented as a scan-surface transform keyed on tool name, server-side, so it applies to stored content already on disk and does not depend on shipping a new hook client.

**3. Give the async lane the same preparation as the inline lane.** Normalize with `NormalizeScanText` and apply the same cap. Worth about 6% from the reformat alone and much more from the cap, and it makes the two lanes' verdicts comparable, which is a prerequisite for option 1. The cap's detection cost is the one already being paid inline today, just extended to the lane that currently absorbs it.

**4. Replace the cap with windowing.** Instead of dropping everything past 50 KiB, scan an oversized response in 50 KiB windows with a small overlap. Costs more tokens than the cap and recovers the coverage the cap throws away. Worth doing on whichever lane survives option 1, and worth pricing against option 2 first, since the edit-family extraction removes most of the oversized population before windowing has to pay for it.

**5. Content-hash the extracted file field.** A per-chat cache keyed on the hash of the echoed file body, so a file is scanned once per chat rather than once per edit. Worth about 72% of an editing session and strictly less than option 2 while carrying more machinery. Only interesting if option 2's detection cost is judged unacceptable, because it is the same saving with the coverage preserved.

Not recommended: message-level content hashing on its own, which the session model puts at 1.9%.

## Follow-up tickets

1. **Split the Presidio ledger by execution path and decide which lane bills.** Run query 1 against production, publish the split, and settle whether `shadow_stream` readings should be billable at all while the lane is labelled shadow. Blocks any forecast built on the current totals.
2. **Meter the inline lane on truncated messages.** `Completed = false` currently means both "do not trust this as a clean checkpoint" and "do not bill this", and the second meaning is wrong: the scan happened. Separate the two so the ledger reflects work performed.
3. **Attribute `tool_name` on `tool_response` readings.** Select `tool_call_id` in `GetMessageContentBatch`, resolve the name from the parent assistant message's `tool_calls`, and carry it into `RiskProvenance`. Prerequisite for every per-tool cost and coverage decision.
4. **Expose tool name to detection scopes on tool responses.** Populate `MessageView.Tools` for `tool_response` from the same resolution as ticket 3, so recommended and policy scopes can say "exempt `Read` output from PII" instead of only "exempt all tool responses".
5. **Add a per-tool scan-surface transform and apply it to the edit family.** Option 2 above. Depends on ticket 3 for the tool name.
6. **Give the async lane the inline lane's preparation.** Option 3 above. Small, self-contained, and it makes the shadow comparison meaningful.
7. **Consolidate the two Presidio lanes.** Option 1 above. The largest saving and the largest change; sequence it after tickets 1, 3, and 6 so the decision is made on measured data and comparable lanes.

## Method

`server/cmd/tools/scanvolume` samples source files from a directory, turns each into one tool response of every shape, and measures each response three ways: raw, after `risk_analysis.NormalizeScanText`, and after `risk_analysis.PrepareScanText`. Token counts come from `stokens.Codec`, the same o200k_base codec the meter uses. `PrepareScanText` and `NormalizeScanText` were extracted from the existing `analyzeOne` body rather than reimplemented, and `TestPrepareScanTextMatchesRequestBody` pins the exported helper to the body the client actually posts, so the harness cannot drift from the scanner.

`docs/research/2026-09-23-tool-response-scan-volume.sql` holds the seven ClickHouse queries that produce the production breakdown: the ledger split by execution path, message type, and hook source; the `tool_name` coverage check; the per-tool size distribution from telemetry; the `originalFile` share of tool-response bytes; and the two dedup granularities. They were validated against the local ClickHouse schema with synthetic rows, so they parse and return the right shapes; the production numbers still need to be filled in by someone with access.

### Caveats

- No production data was read for this write-up. Everything quantified here is either a property of the code, which is cited, or a measurement over a reconstructed corpus, which is labelled as such. The ticket's own figures — 85% of stokens from `tool_response`, 510 to 660M stokens per weekday, half from the responses over 50 KiB — are taken as given and are consistent with the two-lane behaviour described above.
- The harness weights every response shape equally, which production does not. Per-shape and per-field ratios transfer; the corpus totals do not.
- The `Edit` and `MultiEdit` field sets are reconstructed from Claude Code's observable hook output. Query 6 confirms the `originalFile` share directly from production telemetry and should be run before ticket 5 is scoped.
- `hook_source` does not mean quite the same thing in the two tables: on a batch meter reading it is `chat_messages.source`, while in telemetry it is `attributes.gram.hook.source` resolved at ingest, which for Claude is upgraded to the product surface. Expect the label sets to differ.
