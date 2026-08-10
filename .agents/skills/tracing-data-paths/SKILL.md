---
name: tracing-data-paths
description: Use when a task asks to scan, detect, classify, validate, flag, score, or enrich content that the system already stores or receives — "scan X for Y", "detect Y in X", "flag X when Y", "score every X" — or when the implementation is reaching for new state to hold a verdict (status/verdict/scanned_at/flagged columns), a Temporal sweep or backfill over a table, a new scan queue, or a second enforcement point. Also use before writing a migration that adds status columns to an existing table, and when a review says "we already do this", "duplicate work", "two places that can diverge", or "surface the existing result instead".
metadata:
  relevant_files:
    - "server/internal/background/activities/risk_analysis/**"
    - "server/internal/hooks/**"
    - "server/internal/scanners/**"
---

Core principle: **reuse the pipeline, not just the component.** Finding an existing helper to call is not the same as finding out that the data is already being processed. A task phrased as "scan X for Y" is usually a _read_ problem — surface a result the system already computes — not a new pipeline.

The expensive failure is not writing a duplicate scanner. It is building a queue, a state machine, a schedule, an enforcement point, and a UI _around_ a correctly-reused leaf component.

## Trace procedure

Do this before writing any code, including before the migration.

1. **Name the data literally.** "The SKILL.md manifest text", not "skills". You are going to grep for the concrete thing.
2. **Find every write site.** Where does this content enter the system? Grep the ingest/persist path, not the domain package you were handed.
3. **Follow it forward, one hop at a time.** For each store, grep for its readers. Repeat until you hit terminal consumers. Do not stop at the first component you could call — that is the trap.
4. **Ask two questions at each hop.** Does an existing pipeline already process this content? Does an existing model already represent the conclusion I want to store?
5. **Decide what to add last.** Usually: one nullable attribution column, or one case in an existing switch. Schema goes last, never first.

## Gram's content paths

Most "scan content for X" work in this repo is already 90% built. Two routes carry agent/session content; check both before adding anything.

| Route                 | Path                                                                                                                                     | Covers                                                        |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| Async (retrospective) | hook ingest → `chat_messages` / `chat_content_parts` → `risk_analysis` batch → scanners → `risk_results`                                 | tool results, prompts, assistant messages, prompt attachments |
| Sync (blocking)       | [hooks/risk_scan.go](../../../server/internal/hooks/risk_scan.go) → `risk.Scanner.ScanForEnforcement` → per-policy scan → block/warn/ack | prompts and tool **inputs** only                              |

Anchors worth knowing, all in the async route:

- `tool.completed` persists the tool result as a `role: "tool"` chat message — [ingest_hooks.go](../../../server/internal/hooks/ingest_hooks.go) (`persistCanonicalConversationEvent`). Skill activations, MCP calls, and every other tool ride this one event.
- role `tool` → `message.ToolResponse` — [batch_messages.go](../../../server/internal/background/activities/risk_analysis/batch_messages.go) (`messageTypeForRole`)
- the batch then runs gitleaks, Presidio, prompt-injection, and custom rules over it — [risk_analysis/](../../../server/internal/background/activities/risk_analysis/)
- results land in `risk_results`, which already models dead-letter (`dead_letter_reason`), exclusions (`excluded_at`), and the false-positive sweep (`false_positive_at`)

So if content reaches an agent through a tool call or a prompt, it is already scanned under any enabled policy. What is genuinely missing is usually only **attribution** (which domain object produced this finding) and **enforcement point** (sync vs async).

## The findings model is an invariant

A finding exists when a detector found risk. Absence of a finding is not a stored claim of safety.

Never add a durable clean/SAFE verdict per item. It is a probabilistic judgement being recorded as fact, and a false negative then reads as a product guarantee. Storing "unscanned / clean / flagged" also invents a three-state UI where the honest UI has two. Scanner failures belong in dead-letter handling, not in a third verdict value.

## Tells you are rebuilding something

Each of these was present in a real 5-PR stack that duplicated the prompt-injection pipeline. Any one of them means stop and re-run the trace.

- **You are writing defensive machinery to protect a piece of state.** A `ScanStrict` variant so a failed judge cannot record a false clean verdict is a signal the state should not exist, not that it needs hardening.
- **You deferred a feature because it is "coupled to" an existing system's id or model.** Deferring an ack loop because it needs a real policy id means you are standing outside a system you belong inside.
- **You re-derived prose or framing that exists elsewhere.** Hand-rolling a block message that duplicates `renderWarnAgentReason`'s injection-resistant framing.
- **Your first PR is a migration.** Adding columns to a hot, widely-selected table locks the design before anything is validated, and drags struct churn through two model files plus regenerated queries.
- **You are storing a verdict for every row instead of a row per detection.**
- **A schedule you added scans a table another pipeline already sees.**

## Common mistakes

- Grepping for a _component_ to reuse (`NewScanner`), concluding "reuse: done", and never grepping for who reads the data. Component reuse feels like the lazy path while the surrounding rebuild is the actual cost.
- Reading the comment that explains the existing path and not following it. If a file says an event is "layered onto an ordinary tool event", that sentence is the hop.
- Treating coverage differences as bugs to fix rather than product calls to raise. Catalog-wide scanning vs scanning-what-was-used, and always-on vs policy-gated, are decisions for the user — surface them, do not silently pick the one that needs more code.
