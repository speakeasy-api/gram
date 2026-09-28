# Prompt injection confirmation

`risk-prompt-injection-cascade` selects Jev prefiltering followed by Opus
confirmation. It is evaluated locally by organization/project group keys. Create
the PostHog flag disabled before merging; missing flags, lookup failures, and
disabled flags preserve the existing Gemini judge.

Jev returns three Noul probabilities for operational instruction overrides,
guarded-secret extraction, and unauthorized external exfiltration. Any probability
at least 0.50 triggers review. Lower probabilities produce no finding. This matches the evaluated
prefilter cutoff; a false negative at this stage cannot be recovered by Opus. These are
probabilities, not TypeSafe's distinct Choice/Score confidence statistic.

Opus (`anthropic/claude-opus-5.5`) independently reviews the target and at most four
neighbors without seeing the Jev score. Persisted events use two messages before
and two after, ordered by creation time and sequence. Live events with a known
chat use up to four preceding messages. Unlinked events have only the target.
Context queries validate organization, project, and conversation boundaries; a
missing persisted anchor fails the review. Body/tool evidence is bounded, and
truncated bodies are marked. Tool invocations retain names and arguments; stored
tool results retain their actor role, but historical rows may lack a tool name.

Only an Opus-confirmed verdict creates a finding. Jev errors, unavailable context,
malformed responses, rate limits, and Opus errors remain unavailable verdicts,
never completed clean scans. Existing delivery and finding persistence handle
retries; this change adds no topics, workflows, signals, activities, or schedules.
Additional Temporal actions/month: 0. Provider calls scale with scanned messages,
plus candidates meeting the threshold.

The prefilter span records probability, escalation, token counts, and reported
provider cost without raw evidence. Physical-call metrics distinguish Jev and
Opus; Opus keeps the existing verdict/latency metrics. Existing finding APIs,
administrator/member Platform MCP findings tools, and demo seed rows retain their
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
Evaluate the complete labeled corpus and real conversation examples before
enabling the flag for any rollout.

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

Before enabling the flag, rerun the original 1,190-case benchmark, compare models
with equivalent evidence, and report errors separately alongside precision,
overall recall, and in-scope regression recall. Keep additional window fixtures
as a separate slice, distinguish prior-only live evidence from historical
lookahead, and recompute escalation and total cost for the deployed configuration.
