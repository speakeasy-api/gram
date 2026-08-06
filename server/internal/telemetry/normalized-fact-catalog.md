# Normalized Fact Catalog

This is the exhaustive product contract for facts derived from
`telemetry_logs` evidence and written to `telemetry_logs_normalized`.

A fact belongs here only when the product needs to query, display, aggregate,
bill from, or debug that grain directly. Reports, summaries, and time-series
aggregations are computed from canonical facts; they are not stored as
separate fact types.

Changing a fact name, grain, identity, or measure semantics is a data-contract
change. Update this catalog before changing processor code.

## Fact Naming Standard

Every fact name has exactly three lowercase dot-separated segments:

```text
<domain>.<subject>.<occurrence>
```

- `domain`: the product capability that owns the fact contract.
- `subject`: the entity at the fact's grain.
- `occurrence`: what happened or was reported.

Names must match `^[a-z][a-z0-9]*\.[a-z][a-z0-9]*\.[a-z][a-z0-9]*$` — no
underscores, hyphens, uppercase letters, units, or variable depth.

The initial domain vocabulary is frozen; adding a domain is a catalog change:

- `agent`: behavioral events on agent surfaces and Gram-hosted runtimes —
  prompts, responses, observed messages, tool activity, resource reads, and
  skills.
- `usage`: consumption and spend observations, both locally observed and
  provider-settled.
- `evaluation`: evaluation runs and results.
- `automation`: trigger and delivery activity.

Naming rules:

- Provider is a dimension, not a name segment, whenever the fact's semantics
  conform across providers. A provider domain is allowed only for semantics
  that cannot conform.
- Units belong on measures, never in the name: a `cost_usd` measure, not a
  `cost.usd` fact.
- Aggregation periods belong in the grain and dimensions (`bucket_duration`),
  never in the name.
- Physical producer event names never leak into fact names: `tool_result`
  evidence maps to `agent.tool.result`.
- Processing-rule versions live in `rule_version`, never in the name.
- Domains classify fact contracts — consumers, measures, quality, and
  correction semantics — not events. One physical event may feed fact
  contracts in more than one domain: a Gram-hosted completion has an activity
  contract (`agent.response.completed`) while its token measures belong to a
  usage fact.
- User-facing stream names (for example `cursor.usage` on the AI Integrations
  page) are display aliases resolved in the serving layer; they are not
  canonical fact names.

## Common Fact Contract

Every `telemetry_logs_normalized` row must carry:

- `fact_name`: one exact dotted name from this catalog.
- `fact_kind`: `log` or `metric`.
- `fact_key`: a deterministic identity for one exact logical event.
- `organization_id` and `project_id`.
- `event_time`: the timestamp selected by the fact's time rule.
- `first_observed_time` and `last_observed_time`.
- `rule_version`: the processing-rule version that produced the row.
- `quality`: `complete`, `partial`, `invalid`, or `unclassified`.
- `quality_issues`: stable machine-readable reason codes.
- `evidence_ids`: the contributing `telemetry_logs.id` values.
- `dimensions`: typed grouping and filtering values.
- `measures`: typed numeric values and their units.
- `body`: optional human-readable content for log facts.
- `updated_at`: the processor update time used for replacement or coalescing.

Fact keys must remain stable across Pub/Sub retries, out-of-order delivery,
repeated evidence, and processor restarts.

## Exact-event Merge Rule

The first processing layer merges physical evidence only when it proves that it
describes the exact same logical event.

Two rows may share a fact key only when:

1. They map to the same `fact_name`.
2. They carry the same stable identity for that event's documented grain.

Physical producer and channel are not part of the key when different producers
can report the same event. For example, Claude OTel and agenthooks OTel can both
report one `agent.tool.decision`; the agent surface and `tool_use_id` identify
the event.

Related events do not merge merely because they share an operation ID. An
`agent.tool.decision` and `agent.tool.result` with the same `tool_use_id`
remain separate facts because their `fact_name` values differ.

When evidence cannot prove exact identity, it must remain a separate fact. Use
the physical log ID as that fact's key and mark it partial with
`missing_fact_identity`; do not infer equality from trace ID, session ID,
timestamp proximity, or matching dimensions.

## Shared Quality Rules

`complete`

All fields required for the fact grain and every measure required by the
fact definition are present and valid.

`partial`

The grain can be identified and safely exposed, but one or more dimensions,
terminal states, or optional measures expected by the product are absent.

`invalid`

The row matched a processing rule but cannot produce the declared grain or
measure. Invalid evidence must not contribute to product aggregates.

`unclassified`

The physical layout is known, but no processing rule claims it. It remains
visible in the physical Data feed and does not create a normalized fact.

Common quality issue codes:

- `missing_project_id`
- `missing_fact_identity`
- `missing_event_time`
- `missing_session_id`
- `missing_user_identity`
- `missing_provider`
- `missing_model`
- `missing_measure`
- `missing_terminal_status`
- `missing_tool_identity`
- `missing_resource_identity`
- `missing_pricing_source`
- `unsupported_source_variant`

## Canonical Activity Facts

### `agent.tool.decision`

- Product need: permission and policy analytics, blocked-tool views, and
  explanation of why a tool was accepted or rejected.

- Kind: `log`.

- Grain: one final permission decision for one tool invocation.

- Fact key: fact name, organization, agent surface, and
  `gen_ai.tool.call.id` or provider `tool_use_id`.

- If no stable call ID exists, use the physical log ID and mark the fact
  partial. Do not merge by trace, session, tool name, or timestamp.

- Event time: the time the final decision was made.

- Required dimensions: project, agent surface, tool identity, decision
  (`accept` or `reject`), and decision source.

- Optional dimensions: user identity, conversation ID, policy/rule identity,
  block reason, tool source, MCP server/tool identity, tool parameters subject to
  capture policy, and provider event sequence.

- Measures: permission wait duration when the producer reports it.

- Accepted physical evidence:

  - `urn:telemetry:claude:otel:log:tool_decision`
  - `urn:telemetry:codex:otel:log:codex.tool_decision`
  - `urn:telemetry:agenthooks:otel:log:tool_decision`

- Processing: evidence from these layouts shares one fact only when the agent
  surface and call ID identify the same decision. A tool result never updates this
  fact.

- Complete when call identity, tool identity, decision, decision source, project,
  and event time are known.

### `agent.tool.result`

- Product need: executed-tool logs, success/failure rates, latency, result sizes,
  and tool/MCP usage analytics.

- Kind: `log`.

- Grain: one execution result for one accepted tool invocation. A rejected
  decision has no `agent.tool.result`.

- Fact key: fact name, organization, agent surface, and
  `gen_ai.tool.call.id` or provider `tool_use_id`.

- If no stable call ID exists, use the physical log ID and mark the fact
  partial. Do not merge by trace, session, tool name, or timestamp.

- Event time: tool execution completion time.

- Required dimensions: project, agent surface, tool identity, and success/failure
  status.

- Optional dimensions: user identity, conversation ID, error category, result,
  MCP server/tool identity, decision source, tool parameters/input subject to
  capture policy, and provider event sequence.

- Measures: execution duration milliseconds, input bytes, result bytes, and
  result tokens when present.

- Accepted physical evidence:

  - `urn:telemetry:claude:otel:log:tool_result`
  - `urn:telemetry:agenthooks:otel:log:tool_result`

- Processing: evidence from these layouts shares one fact only when the agent
  surface and call ID identify the same result. A tool decision never updates this
  fact. Gram's current `gram:otel:log:tool_call` is not accepted until it
  carries an identity and semantics that prove it is the same result event.

- Complete when tool identity, fact identity, event time, project, and terminal
  status are known.

### `agent.resource.read`

- Product need: resource activity, MCP resource debugging, and separation of
  read-only access from tool execution.

- Kind: `log`.

- Grain: one resource read attempt.

- Fact key, in order:

  1. Organization and producer resource-read ID.
  2. Project and physical log ID, with `missing_fact_identity`.

- Event time: resource read attempt time.

- Required dimensions: project, resource URN or target reference, and status.

- Optional dimensions: target kind, MCP server identity, tool identity, user
  identity, deployment, trace ID, and external user identity.

- Measures: duration in milliseconds, status code, request bytes, and response
  bytes.

- Accepted physical evidence:

  - `urn:telemetry:gram:otel:log:resource_read`

- Processing: only evidence explicitly representing the same resource-read event
  may share a fact key. Tool decisions and results remain separate facts.

- Complete when resource identity, project, event time, and status are known.

### `agent.prompt.submitted`

- Product need: message/turn counting, prompt history, session correlation, and
  data-quality verification of user-originated input.

- Kind: `log`.

- Grain: one submitted user prompt.

- Fact key, in order: producer prompt ID, message ID, organization plus agent
  surface plus conversation ID plus producer sequence, then physical log ID.

- Event time: producer prompt timestamp.

- Required dimensions: project, agent surface, conversation ID, and prompt or
  message identity.

- Optional dimensions: user identity, model, hostname, query source, and prompt
  size. Prompt content follows capture and redaction policy and is not required.

- Measures: prompt bytes and attachment count when present.

- Accepted physical evidence:

  - `urn:telemetry:claude:otel:log:user_prompt`
  - `urn:telemetry:codex:otel:log:codex.user_prompt`
  - `urn:telemetry:agenthooks:otel:log:userpromptsubmit`
  - `urn:telemetry:agenthooks:otel:log:beforesubmitprompt`

- Complete when prompt identity, conversation ID, project, and event time are
  known.

### `agent.response.completed`

- Product need: model-response history and linking Gram-hosted responses to usage,
  cost, evaluations, assistants, and conversations.

- Kind: `log`.

- Grain: one completed model response authored through a Gram-hosted completion
  surface.

- Fact key, in order: `gen_ai.response.id`, message ID, conversation ID plus
  producer sequence, then physical log ID.

- Event time: response completion time.

- Required dimensions: project, model, conversation ID, and response identity.

- Optional dimensions: provider, user identity, assistant ID, runtime ID, message
  ID, finish reason, and hook source.

- Measures: latency milliseconds and response bytes. Token and cost measures
  belong to `usage.response.observed` and are linked, never independently summed.

- Accepted physical evidence:

  - `urn:telemetry:gram:otel:log:chat_completion`

- Complete when response identity, model, conversation ID, project, and event time
  are known.

### `agent.skill.activation`

- Product need: skill adoption, skill usage, version analysis, and skill-efficacy
  correlation.

- Kind: `log`.

- Grain: one activation of one skill in one agent session.

- Fact key, in order: explicit activation ID, tool-call ID when one call
  identifies one activation, then physical log ID with
  `missing_fact_identity`.

- Event time: explicit or inferred activation time.

- Required dimensions: project, session ID, skill name, and agent surface.

- Optional dimensions: skill version, tool-call ID, user identity, model, query
  source, and whether activation was explicit or inferred.

- Measures: none.

- Accepted physical evidence:

  - `urn:telemetry:agenthooks:otel:log:skill.activated`

- Processing: evidence shares a fact key only when an activation ID or tool-call
  ID proves it describes the same activation.

- Complete when skill name, session ID, project, and event time are known.

### `agent.message.observed`

- Product need: Claude Chat web/desktop Agent Sessions and compliance review.

- Kind: `log`.

- Grain: one upstream provider-observed chat message.

- Fact key: AI integration config generation and upstream message ID.

- Event time: upstream message creation time.

- Required dimensions: organization, project, provider, chat ID, message ID,
  role, and surface (`claude-chat-web` or `claude` desktop).

- Optional dimensions: user identity, model, attachments, parent message, and
  content metadata.

- Measures: content bytes and attachment count when present.

- Source evidence: Anthropic Compliance API chat messages. These currently enter
  through the chat persistence path rather than `telemetry_logs`; the
  `telemetry_logs_normalized` implementation must either publish equivalent evidence or
  explicitly keep this stream outside the first processor.

- Complete when chat, message, role, project, and event time are known.

## Canonical Evaluation Facts

### `evaluation.subject.result`

- Product need: quality, safety, and automated evaluation reporting.

- Kind: `log`.

- Grain: one named evaluation result for one evaluation run and subject.

- Fact key: evaluation run ID, evaluation name, and subject identity. Fall back
  to the physical log ID only with `missing_fact_identity`.

- Event time: evaluation completion time.

- Required dimensions: project, evaluation name, subject identity, and either
  score label or score value.

- Optional dimensions: model, conversation ID, user identity, evaluator version,
  and explanation.

- Measures: numeric score when present.

- Accepted physical evidence:

  - `urn:telemetry:gram:otel:log:chat_resolution`
  - `urn:telemetry:gram:otel:log:evaluation`

- For `chat_resolution`, allowed labels are `success`, `failure`, `partial`, and
  `abandoned`.

- Complete when evaluation name, subject, result, project, and event time are
  known.

## Canonical Automation Facts

### `automation.trigger.delivery`

- Product need: trigger observability, delivery status, retries, and workflow
  reliability.

- Kind: `log`.

- Grain: one trigger delivery attempt to one target.

- Fact key: trigger event ID, target reference, and attempt number.

- Event time: delivery attempt time.

- Required dimensions: project, trigger event ID, target kind, target reference,
  and delivery status.

- Optional dimensions: trigger definition, trigger instance, correlation ID,
  assistant ID, and error class.

- Measures: duration milliseconds and retry number.

- Accepted physical evidence:

  - `urn:telemetry:gram:otel:log:trigger`

- Complete when trigger event, target, status, project, and event time are known.

## Conformed Usage Vocabulary

The spend/usage cube, session summaries, and Tokens Under Management union the
two usage fact types below into one measure set. Both fact types must therefore
carry the same conformed dimensions and measures. This shared vocabulary is the
load-bearing contract; provider variants must not drift from it.

Conformed dimensions:

- Organization and project.
- User identity: user email and user ID, plus enriched directory attributes
  (department, division, job title, employee type, cost center, roles, and
  groups).
- Agent surface (`hook_source`): the consuming surface, such as `claude-code`,
  `cursor`, `codex`, or `cowork`. Agent surface is neither producer nor
  provider and remains a first-class dimension.
- Provider.
- Model.
- Account type and billing mode.
- Authority: `locally_observed` for telemetry captured at response time, or
  `provider_settled` for provider-reported records.

Attribution dimensions (MCP server, MCP tool, skill, subagent, and query
source) apply to `usage.response.observed` only; provider-settled records do
not carry them.

Conformed measures, disjoint by definition:

- `input_tokens`: excludes cache reads.
- `output_tokens`.
- `cache_read_tokens`.
- `cache_write_tokens`.
- `cost_usd`: computed or provider-reported model cost. Provider-charged
  amounts (for example Cursor's charged amount) are kept as a distinct measure
  and never conflated with computed model cost.

Normalization rules:

- Never trust a producer-reported total; totals are always computed from the
  disjoint components.
- Codex input counts include cached input and must be normalized into disjoint
  input and cache-read measures, clamped so malformed client data can never
  increase usage.
- The Tokens Under Management measure is input plus output plus cache writes;
  cache reads are excluded everywhere.

Serving rule: `usage.response.observed` and `usage.provider.reported` can
describe the same underlying activity from two authorities. Gold read models
must select one population per provider per window, or reconcile them
explicitly, and must never sum both. The current URN-prefix union cannot
express this distinction and can double-count a provider observed through both
hooks and a provider API integration.

User-identity fallbacks (for example email falling back to device hostname)
are serving-layer display policy. They are never part of fact identity or
stored dimensions.

## Canonical Usage Facts

### `usage.response.observed`

- Product need: Tokens Under Management, employee insights, session usage, model
  breakdowns, cost explorer, response-level observed cost, and observed-agent
  utilization.

- Kind: `metric`.

- Grain: one provider response or usage point observed from an agent surface at
  response time.

- Authority: `locally_observed`.

- Fact key, in order: provider request ID, provider response ID, upstream event
  hash, then producer plus conversation ID plus source event ID.

- Event time: provider response time, otherwise physical event time.

- Required dimensions: project, agent surface, provider, model, and fact
  identity.

- Optional dimensions: organization, conversation ID, user identity, hostname,
  account type, billing mode, external provider organization, query source,
  skill, subagent name, MCP server, MCP tool, and pricing source when cost is
  present.

- Measures: the conformed token measures. Cost measures (`cost_usd` and
  input/output/cache cost splits) ride this same fact when the same response
  event reports provider cost, as Claude `api_request` does; there is no
  separate response-cost fact.

- Accepted physical evidence:

  - `urn:telemetry:claude:otel:log:api_request`
  - `urn:telemetry:codex:otel:log:codex.sse_event` when
    `event.kind=response.completed`
  - `urn:telemetry:agenthooks:otel:log:usage.reported`
  - `urn:telemetry:agenthooks:otel:log:usage`
  - `urn:telemetry:gram:otel:log:chat_completion` when it carries usage

- Processing: physical evidence shares a fact key only when the same stable
  provider request, response, or upstream event ID proves it reports the exact
  usage event. Codex input normalization applies (see the conformed
  vocabulary). Gram-hosted rows carry the Gram-hosted agent surface and are
  excluded from Tokens Under Management by population filter, not by omission
  from the fact table. Duplicate derived Claude/Codex usage rows are excluded.

- Complete when identity, provider, model, project, event time, and at least one
  token measure are known. Exposing cost measures as spend additionally requires
  billing mode and pricing source.

### `usage.provider.reported`

- Product need: organization-level provider usage and spend from provider APIs —
  Cursor usage and spend, Claude Chat token usage and spend, and Codex spend —
  plus AI Integrations stream health and settled-spend reporting.

- Kind: `metric`.

- Grain: one provider-reported usage or spend record exactly as supplied by the
  provider API — an individual event or an aggregated bucket, per provider.

- Authority: `provider_settled`.

- Fact key: fact name, project, provider, and the provider's dedup hash:

  - Cursor: `cursor.event_hash`.
  - Claude Chat: `claude_chat.event_hash`.
  - Codex: `codex.compliance.event_hash`.

- Event time: upstream record time.

- Required dimensions: project, provider, upstream record hash, and provider
  surface (`cursor`, `claude-chat-web`, `claude` desktop, or `codex`).

- Optional dimensions: user identity, model, team/workspace/account, billing
  classification, provider organization, and `bucket_duration` when the provider
  aggregates.

- Measures: sparse by provider — the conformed token measures and/or cost:

  - Cursor: tokens, model cost, and charged amount, kept distinct.
  - Claude Chat: usage records carry tokens; cost records carry cost.
  - Codex: cost. Preserve raw credits and conversion rate when the source unit
    is `CREDITS`.

- Accepted physical evidence:

  - `urn:telemetry:cursor:api:metric:usage`
  - `urn:telemetry:claude:api:metric:chat.usage.tokens`
  - `urn:telemetry:claude:api:metric:chat.cost.usd`
  - `urn:telemetry:codex:api:metric:cost.usd`

- Processing: records merge only on an identical provider hash. Usage and cost
  records the provider supplies separately (Claude Chat) remain separate facts
  sharing this fact name, not a fact key. Measures are state: a provider
  restating a bucket updates the existing fact rather than creating a new one.

- Serving: never summed with `usage.response.observed` for the same provider —
  see the conformed usage vocabulary.

- Complete when hash, provider, project, event time, and at least one usage or
  spend measure are known.

## Derived Product Views, Not Fact Names

The following product outputs are computed from canonical facts and must not
be inserted as duplicate event grains:

- Tokens Under Management by day and dimension derives from the usage facts
  under the conformed serving rule; cache reads are excluded.
- Tool decision analytics derive from `agent.tool.decision`.
- Executed-tool usage totals, success/failure rates, target breakdowns, user
  breakdowns, and time series derive from `agent.tool.result`.
- Session lists and session summaries correlate exact lifecycle, prompt, usage,
  and tool facts at query time; there is no canonical session fact in this
  first processing layer.
- Hook summaries query physical agenthooks lifecycle events plus
  `agent.tool.decision`, `agent.tool.result`, and `agent.skill.activation`;
  there is no generic agenthooks event fact.
- Skill efficacy aggregates join `agent.skill.activation` with the separate
  skill-efficacy result domain.
- Shadow MCP inventory remains a separate read model over physical evidence;
  there is no canonical MCP-server-observed fact in this layer.
- AI Integrations stream tiles (for example `cursor.usage`) are display
  aliases over `usage.provider.reported` filtered by provider and surface.

## Explicitly Outside This Catalog

These product facts use separate authoritative stores and are not derived by the
`telemetry_logs` Pub/Sub processor:

- Risk findings in `risk_findings`.
- Authorization challenges in `authz_challenges`.
- Audit events in the audit logging subsystem.
- Skill-efficacy judge results in `skill_efficacy_scores`.
- AI integration configuration and synchronization health in Postgres.

If any of these move into `telemetry_logs_normalized`, add their exact grains here before
implementation.

## Name Changes From Earlier Drafts

Earlier drafts of this catalog used pre-standard names. The mapping is:

- `tool.decision` → `agent.tool.decision`
- `tool.result` → `agent.tool.result`
- `resource.read` → `agent.resource.read`
- `agent.prompt` → `agent.prompt.submitted`
- `chat.completion` → `agent.response.completed`
- `skill.activation` → `agent.skill.activation`
- `claude.chat.message` → `agent.message.observed`
- `evaluation.result` → `evaluation.subject.result`
- `trigger.delivery` → `automation.trigger.delivery`
- `agent.usage.tokens` and `agent.cost.usd` → `usage.response.observed`
- `cursor.usage`, `claude.chat.usage.tokens`, `claude.chat.cost.usd`, and
  `codex.cost.usd` → `usage.provider.reported`

## Adding or Changing a Normalized Fact

Before implementation:

1. Add the exact stable dotted name and confirm it matches the naming standard
   and belongs to a frozen domain.
2. State the product consumer and why the grain is needed.
3. Define one unambiguous grain.
4. Define the deterministic fact-key formula and fallback order.
5. Define event-time selection.
6. List exact dimensions with attribute keys, types, and sensitivity, reusing
   the conformed usage vocabulary where applicable.
7. List exact measures with types, units, and aggregation semantics.
8. List exact accepted physical URNs.
9. Define the exact-event identity rule and which physical layouts can report
   that same event.
10. Define complete, partial, and invalid outcomes with quality issue codes.
11. Define deletion, correction, and rule-version behavior.
12. Add complete, partial, duplicate, and repeated-evidence fixtures.
13. Name every dashboard, API, billing calculation, or read model that consumes
    the fact.
