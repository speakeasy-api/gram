# Prompt injection confirmation

Prompt-injection scanning uses Jev prefiltering followed by Opus confirmation.
The cascade runs for every scan; there is no rollout flag or Gemini fallback.

Jev returns three Noul probabilities for operational instruction overrides,
guarded-secret extraction, and unauthorized external exfiltration. Any probability
at least 0.50 triggers review. Lower probabilities clear only complete evidence;
if rendering or input-budget reduction truncated any evidence, the result is
unavailable and incomplete instead. This matches the evaluated
prefilter cutoff; a false negative at this stage cannot be recovered by Opus. These are
probabilities, not TypeSafe's distinct Choice/Score confidence statistic.

Jev input uses a 28,000 estimated-token budget across serialized state and all
questions, leaving headroom below the documented 32k-token context limit. The
estimate is Unicode characters divided by four, rounded up, including JSON
structure and escaping. No public Jev tokenizer is documented; this English-text
heuristic can undercount JSON, code, and non-English content. It is not an exact
provider token count. The largest evidence fields are shortened first, retaining
head and tail text on UTF-8 boundaries and marking truncation. JSON structure,
question wording, and attribution structure are preserved; shortened tool identities
are explicitly marked. Cleared-scan token
accounting uses the evidence actually sent. Truncation is recorded in tracing.

On a recognized structured context overflow, retry Jev once with an
estimated-token budget 20% below the rejected state plus questions. Questions
stay unchanged; evidence is truncated further. Both attempts share the 10-second
prefilter deadline, and each physical attempt is recorded. A second failure
returns unavailable.

Recognized errors are OpenRouter's documented `context_length_exceeded` and the
Jev Decisions response verified with synthetic inputs on 2026-09-28: HTTP 400,
`error.code = 400`, with `error.message` containing `HTTP 400: ` followed by
JSON `{"detail":{"error_type":"max_tokens_exceeded"}}`. This nested Jev input
error is distinct from a generic OpenRouter output-limit error. The decoder
parses the complete JSON wrapper; it never matches arbitrary message substrings.
Generic HTTP errors, rate limits, and credit/token-budget errors do not retry.

The live probe rejected 38,000 repetitions of a synthetic greeting. Reducing
that input by 20% to 30,400 repetitions succeeded (30,708 reported input tokens).
These probes verify the transport error shape, not detection accuracy.

Known limitation: an attack in omitted text can be missed by the prefilter and
never reach Opus. Chunking or a model-specific tokenizer could improve coverage
later. Opus evidence remains independently prepared under its existing limits.

Opus (`anthropic/claude-opus-5.5`) independently reviews the target and at most four
neighbors without seeing the Jev score. Persisted events use two messages before
and two after, ordered by creation time and sequence. Live events with a known
chat use up to four preceding messages. Unlinked events have only the target.
Context queries validate organization, project, conversation, and generation boundaries; a
missing persisted anchor fails the review. Neighbors include at most eight tool calls with 4,000-rune arguments; truncation is explicit.
Serialized confirmation evidence is capped at 256 KiB; oversized requests are unavailable.
Body/tool evidence is bounded, and
truncated bodies are marked. Tool invocations retain names and arguments; stored
tool results retain their actor role, but historical rows may lack a tool name.

Only an Opus-confirmed verdict creates a finding. Jev errors, unavailable context,
malformed responses, provider throttling, and Opus errors remain unavailable verdicts,
never completed clean scans. Existing delivery and finding persistence handle
retries; this change adds no topics, workflows, signals, activities, or schedules.
Additional Temporal actions/month: 0. Provider calls scale with scanned messages,
plus candidates meeting the threshold.

Concurrent cases are bounded, but PI calls do not use the shared Redis judge
rate limiter. Provider throttling remains an unavailable result; the benchmark
runs the same path without a Redis emulator.

The prefilter span records probability, escalation, token counts, and reported
provider cost without raw evidence. Physical-call metrics distinguish Jev and
Opus; Opus keeps the existing verdict/latency metrics. Existing finding APIs,
Platform MCP findings tools, and demo seed rows retain their
contracts; no new tool, permission, or seed shape is needed.

## Evaluation

Run `mise exec -- go run ./server/cmd/risk-pi-report -cascade` from the repository
root with `OPENROUTER_DEV_KEY` configured. The report uses production orchestration
and records confirmation calls, prefilter misses, total provider cost, latency,
precision, and recall. Run without `-cascade` for the baseline. JSONL cases can
provide a `window` with up to five rendered messages and a `target_index`; cases
without a window evaluate the target with its trajectory. Both Jev and Opus
receive the same bounded trajectory used by the Gemini baseline. Conversation
windows supplement that trajectory when available; historical and live window
results must be reported separately.

`-sources cascade_context -check-floors=false` runs four synthetic conversation
smoke cases. These check transport and composition, not representative accuracy.
Evaluate the complete labeled corpus and real conversation examples to measure
precision, recall, and provider failures.

## Research and rollout evidence

[Evaluating Jev for Prompt Injection](https://jev-pi-evaluation.david232314.chatgpt.site/)
records the flag-rate study, cost assumptions, labeled benchmark, and
[case review](https://jev-pi-evaluation.david232314.chatgpt.site/review/).
The 0.50 cutoff matches the research baseline. In the enriched 10,000-message
sample it escalated 27 messages; 0.90 escalated none. The sample deliberately
included historical positives, so these are sample escalation rates, not natural
production prevalence or accuracy measurements.

The report's Opus 5.5 benchmark had 155 errors counted as unflagged. Its costs
include planning estimates, and its cached cascade did not exercise Jev-error
fallback. This implementation returns unavailable on Jev errors. The research
motivates further evaluation; it does not establish rollout readiness for this
implementation's revised Jev instructions, confirmation window, or timeout.

To validate the migration, rerun the original 1,190-case benchmark, compare models
with equivalent evidence, and report errors separately alongside precision,
overall recall, and in-scope regression recall. Keep additional window fixtures
as a separate slice, distinguish prior-only live evidence from historical
lookahead, and recompute escalation and total cost for the deployed configuration.

## Platform MCP assessment

Outcome and resource: inspect stored prompt-injection findings through
`list_watchdog_findings`. The external actor is an authenticated organization
administrator; the tool also serves managed assistants under their existing
project scope. Its bounded, redacted rule-level alerts already represent the
findings produced by this scanner.

Decision: intentionally omit a Platform MCP tool change. Jev prefiltering and
Opus confirmation change internal classification, without adding a management
operation or changing finding schemas, authorization, project selection, or
redaction. The existing tool and the shipped
`summarize-critical-watchdog-findings` skill remain applicable; exposing raw
classification evidence or model controls would add an unnecessary internal
surface.

Success evidence: `TestCascadeConfirmedInjection` and
`TestCascadeOpusFailureIsUnavailable` cover confirmed findings and unavailable
reviews. `TestRiskFindingsMCPInProcess`, `TestRiskFindingsEvidence`, and
`TestRiskFindingsValidationAndGates` cover the existing MCP result, redaction,
and access/feature boundaries. No Platform MCP schema or shipped workflow needs
to change for these internal classifier decisions.
