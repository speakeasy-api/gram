# Prompt injection

The prompt-injection scanner classifies a message (its text, tool name and
tool-call arguments, plus a bounded trajectory of the prior user request and
the most recent untrusted content) with an LLM judge and emits one
`prompt_injection` finding when the verdict is `INJECTION`. `Scanner` wraps a
`Classifier`; production wires `openrouter.Engine.Classify`, tests wire
`NoopClassifier`.

The judge fails open. A throttle, timeout or provider outage yields
`UNAVAILABLE`, which produces no finding and `Completed = false`, so an outage
never blocks a message and never records content as judged clean.

## Where it runs

| Lane                   | Process | Entry point                                                           | Engine                    |
| ---------------------- | ------- | --------------------------------------------------------------------- | ------------------------- |
| Realtime enforcement   | server  | `risk.Service.scanPolicy` (`internal/risk/scanner.go`)                | in-process `Scanner`      |
| Skill manifest capture | server  | `hooks.Service` (`internal/hooks/upload_skill_content.go`)            | in-process `Scanner`      |
| MCP research agent     | worker  | `researchagent.ScannerJudge`                                          | in-process `Scanner`      |
| **Batch analysis**     | streams | `promptinjection.Handler` on `gram-risk-v1-prompt-injection-analyzer` | consumer-only (see below) |

## Batch analysis is Pub/Sub only

`AnalyzeBatch` (Temporal, worker) publishes one `PromptInjectionAnalysis` per
in-scope message and does not scan. The worker holds no prompt-injection judge
for risk analysis and pays none of its latency; the streams consumer is the
only engine, so it has no `AsyncShadowGate` and runs the real classifier for
every request.

```
AnalyzeBatch (worker) ─► scanStandardPolicy
   │  sources ∋ prompt_injection, LLM analyzer mode ≠ llm
   ▼
publishPromptInjectionScanRequests: one request per in-scope message
   │  execution_path = prompt_injection_stream
   ▼  topic gram-risk-v1-prompt-injection-analysis (7 d retention)
streams: promptinjection.Handler (ack 60 s, retry 10 s–600 s, no DLQ)
   │  Scanner.ScanWithVerdict + RiskRecorder.Record
   ▼
scanners.PublishFindings ─► Finding topic ─► FindingCHWriter ─► ClickHouse risk_findings
```

The publish must succeed for the activity to succeed. Request ids are
deterministic over the batch identity and finding ids derive from them, so a
Temporal redrive republishes under the same ids instead of duplicating rows.

`AnalyzeBatch` still writes the per-message sentinel row
(`source = 'none'`, `found = false`) for every scanned unit, so "messages
analyzed" progress is unaffected.

### Consumer capacity and judge budget

Total judge demand goes **down**: the batch used to make one inline call per
message plus one consumer call for the sampled share, and now makes one
consumer call per message. Both lanes already drew from the same fleet-wide
bucket (`openrouter.NewJudgeRateLimiter`, Redis-backed, 250/min with a burst
of 50, keyed per model for platform keys and per key+model for BYOK), which
the realtime lane shares.

What changes is that a denied call is no longer backstopped by an inline scan:
it fails open into a message nothing judged. Two things guard that:

- The consumer's receive settings cap outstanding messages at 64 per replica
  (`promptInjectionReceiveSettings` in `server/cmd/gram/streams.go`) instead of
  the client default of 1000, which is roughly twenty times what the limiter
  sustains. Excess waits on the subscription — 7 day retention — instead of
  burning tokens on fail-open verdicts.
- `risk.async_scan.handler_messages{scanner=prompt_injection}` records
  `outcome = no_verdict` for exactly these messages, apart from `ok`. Watch it
  when sizing the judge budget against real scan volume.

### Postgres readers

Batch prompt-injection findings no longer land in Postgres `risk_results`;
they exist only in ClickHouse `risk_findings`. This is the same trade the LLM
analyzer's async lane already makes for its covered sources (see
`../llmanalyzer/README.md`, "Known POC limits"). Where each reader is served:

| Reader                                                                                                                             | Served by                                                                                           |
| ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| Project risk events list, overview KPIs, unmask/reveal, dismissed tab                                                              | ClickHouse, **requires** `risk-list-from-clickhouse` and `risk-overview-from-clickhouse` on the org |
| Watchdog signals, grouped Platform MCP findings, retro exclusions                                                                  | ClickHouse already                                                                                  |
| Policy "messages analyzed" progress                                                                                                | Postgres sentinel rows, unchanged                                                                   |
| Realtime enforcement decisions and `tool_call_blocks`                                                                              | in-process realtime scan, unchanged                                                                 |
| Skill-manifest prompt-injection findings                                                                                           | the hooks skill path, which writes its own rows, unchanged                                          |
| Chat transcript badges and counts, per-chat listing, session recall masking, user/rule breakdowns, `risk.finding.created` webhooks | **not served** — Postgres-only surfaces with no ClickHouse read path yet                            |

The last row is the cost of the cutover and the reason an org must be on the
ClickHouse read flags before it matters to them. Closing it means giving those
surfaces a ClickHouse read path, not restoring the inline scan: the consumer
cannot write `risk_results` itself because the policy filters the batch applies
after scanning — exclusions, disabled rules, built-in false-positive presets
and CEL detection scopes — are not carried on the analysis request.
