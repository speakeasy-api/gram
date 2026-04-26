# Agentic AI Insights — Future Work / YAGNI Backlog

**Date:** 2026-04-24
**Companion to:** `2026-04-24-agentic-ai-insights-design.md`
**Status:** Backlog — items intentionally cut from v1 to keep the first ship small

## Why this exists

The v1 design deliberately ships a narrow slice of "agentic" capability: B (deeper analysis on demand), C1+C2 (tool / toolset proposals), D (workspace memory) — plus rollback. Everything below was _considered and explicitly cut_ during the brainstorm. They're filed here so we have a single home to come back to once v1 ships, has user feedback, and we know which gaps actually matter.

Each entry has the same shape:

- **What** — the cut feature
- **Why deferred** — what made it not worth shipping in v1
- **Trigger to revisit** — the user-visible signal that should pull it out of the backlog

## The backlog

### A. Proactive / scheduled insights

- **What:** Insights runs on a schedule (e.g. nightly digest) or on event triggers (post-deploy, error-rate spike). Surfaces a "Top 3 things worth your attention this week" digest in the sidebar, via email, or via Slack.
- **Why deferred:** v1 user explicitly chose B/C/D, not A. Scheduled runs need a Temporal workflow, digest storage, delivery channels, and rate-limiting. Each piece is straightforward but together they're a separate project.
- **Trigger to revisit:** v1 users ask for "tell me when something breaks" or "I forgot to look at insights for two weeks and missed an obvious problem."

### A2. Slack / email digest delivery

- **What:** First-class delivery channels for proactive digests. Reuse the existing PostHog/Slack CDP function pattern noted in `reference_posthog.md`.
- **Why deferred:** Pointless without (A) — there's nothing to deliver yet.
- **Trigger to revisit:** ships immediately after (A).

### Vector embeddings for memory recall

- **What:** Replace tag + recency ranking in `gram_recall_memory` with an embedding-based semantic search. Store embeddings alongside `insights_memories` rows.
- **Why deferred:** v1 ranking is good enough for low-volume per-project memory (estimated <500 rows per active project). Adds an embedding model dependency, a vector index (pgvector or extra ClickHouse table), and a cost line item.
- **Trigger to revisit:** users complain that obvious memories aren't being recalled, OR a workspace crosses ~1k active memories and tag overlap stops being discriminative.

### Multi-reviewer / approval-queue workflow

- **What:** Critical proposals (esp. toolset changes) require N approvals before Apply is enabled. CODEOWNERS-style routing to the right reviewers.
- **Why deferred:** No customer demand yet. v1's audit log + per-project Editor scope is the security boundary; if Editors trust each other, single-click Apply is fine.
- **Trigger to revisit:** an enterprise customer asks for it, OR we see an "oh god the agent shipped a bad change" incident in our own dogfooding.

### Sub-agents / multi-agent orchestration

- **What:** Break the monolithic Insights agent into specialists (LogAnalyst, ToolQualityCritic, UsagePatternFinder) with an orchestrator deciding which to invoke. More tokens spent, theoretically better answers.
- **Why deferred:** The single-agent + structured prompt + new tools approach captures most of the value at a fraction of the complexity. Multi-agent orchestration also makes investigation traces harder to follow and debug.
- **Trigger to revisit:** a clear category of investigation that single-agent demonstrably fails at (e.g. needing to hold log search state AND tool-call analytics state simultaneously beyond context budget).

### C3 — Environment & deployment actions

- **What:** Agent can propose env variable changes, rollback a deploy, change global variation defaults, etc.
- **Why deferred:** Highest blast radius in the action surface. v1 user explicitly scoped down to tool-level (C1) and toolset-level (C2). Each new action target needs its own staleness/diff/revert story.
- **Trigger to revisit:** v1 has been stable for 4+ weeks AND there's a documented user need ("I want the agent to flag broken env vars and propose a fix").

### C4 — External-system actions

- **What:** Agent can file GitHub issues, post to Slack channels, create Linear tickets when it finds something the human team should triage.
- **Why deferred:** Less risky than C3 (nothing in Gram changes), but requires per-org integration plumbing (GitHub app install, Slack webhook config, Linear API key). Each integration is a small project.
- **Trigger to revisit:** v1 users start manually copy-pasting Insights findings into their issue tracker. That's the signal that it's worth automating.

### Cross-project / organization-level memory

- **What:** Memories scoped to the org, not the project. Useful when one customer has 5 projects with shared glossary ("we always call this thing X").
- **Why deferred:** Adds a scope dimension to every memory query, plus org-level RBAC for who can read/write org memories. v1 ships per-project — simpler, more local, and probably correct for most teams.
- **Trigger to revisit:** orgs with 3+ active projects ask for it, OR we see the agent rediscovering the same fact independently in different projects of the same org.

### Toolset reordering proposals

- **What:** `proposeToolsetChange` accepts a `reorder` operation (move tool X from position 3 to position 1).
- **Why deferred:** Order matters less than presence in current MCP semantics. Add when there's a clear quality story tying tool order to behavior.
- **Trigger to revisit:** evidence (from chat resolutions or tool-call analytics) that ordering meaningfully changes how clients pick tools.

### "Edit then apply" UX

- **What:** User can tweak a proposed value before clicking Apply. The agent suggests; the human polishes.
- **Why deferred:** The data model already supports it (`applied_value` is separate from `proposed_value`), but the UI is non-trivial — diff editing inline. v1 ships pure Apply / Dismiss.
- **Trigger to revisit:** users say "the agent's proposal was 90% right but I can't tweak it" — that's the trigger.

### Investigation playbooks

- **What:** Pre-canned templated investigations the user can launch in one click ("Diagnose tool X failures", "Audit deprecated tool usage"). Each is a structured multi-step prompt the agent follows.
- **Why deferred:** v1 already has the investigation protocol in the system prompt; canned playbooks are an ergonomics win, not a capability win. Wait until we see which investigations users repeat.
- **Trigger to revisit:** chat resolution analytics show the same kind of question being asked repeatedly across users — that's a playbook candidate.

### Confidence scoring on proposals

- **What:** Each proposal carries a confidence score (low / medium / high) the model self-reports. UI uses it to sort / filter the inbox.
- **Why deferred:** Self-reported confidence is unreliable without calibration data. v1 trusts the user to skim and decide.
- **Trigger to revisit:** users say "too many proposals, I can't tell which ones to read first" — and we have at least N apply/dismiss outcomes to calibrate against.

### Auto-grouping of related proposals

- **What:** If the agent proposes 4 variation edits to the same toolset in one investigation, group them as a single bundle the user can Apply All / Dismiss All.
- **Why deferred:** Premature; no data on whether bundles are common.
- **Trigger to revisit:** observed pattern of multi-proposal investigations.

### Cost / usage analytics for the agent itself

- **What:** A small dashboard panel showing "Insights used X tokens this week, applied Y proposals, rolled back Z." Helps operators justify the feature and spot runaway loops.
- **Why deferred:** Premature. Nice-to-have once the feature is broadly used.
- **Trigger to revisit:** finance asks how much the agent costs, OR we have a runaway loop incident.

### A/B testing variations before applying

- **What:** Apply a proposed variation only to a fraction of MCP traffic, measure resolution rate, then auto-promote or roll back.
- **Why deferred:** Requires variation-level traffic splitting infrastructure, statistical machinery, and a longer feedback loop than v1 supports.
- **Trigger to revisit:** users start asking "but how do I know the agent's suggested description is actually better?"

### Auto-pruning runaway memories

- **What:** Beyond the 90-day expiry, detect and prune duplicates / contradictions / never-recalled memories.
- **Why deferred:** v1's `usefulness_score` + 90-day expiry should be enough at the volumes we expect. Add only if memory quality degrades visibly.
- **Trigger to revisit:** memory quality complaints from users.

### Multi-locale / multi-language insights

- **What:** Per-locale memory, per-locale agent system prompts.
- **Why deferred:** No customer signal yet.
- **Trigger to revisit:** non-English-speaking customer asks.

## Process note

Whenever we revisit one of these, run it through the same brainstorm-and-design loop the v1 went through. Don't ship from this backlog directly — the world will have changed, and the cheapest design today may not be the cheapest design tomorrow.
