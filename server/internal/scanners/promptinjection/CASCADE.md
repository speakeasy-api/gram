# Prompt injection confirmation

Prompt-injection scanning uses Jev prefiltering followed by Sonnet confirmation.
The cascade runs for every scan; there is no rollout flag or Gemini fallback.

Jev returns three Noul probabilities for operational instruction overrides,
guarded-secret extraction, and unauthorized external exfiltration. Any probability
at least 0.50 triggers review. Lower probabilities clear only complete evidence;
if rendering or input-budget reduction truncated any evidence, the result is
unavailable and incomplete instead. This matches the evaluated
prefilter cutoff; a false negative at this stage cannot be recovered by the confirmer. These are
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
never reach the confirmer. Chunking or a model-specific tokenizer could improve coverage
later. Confirmation evidence remains independently prepared under its existing limits.

Sonnet (`anthropic/claude-sonnet-5.5`) independently reviews the target and at most four
neighbors without seeing the Jev score. Persisted events use two messages before
and two after, ordered by creation time and sequence. Live events with a known
chat use up to four preceding messages. Unlinked events have only the target.
Context queries validate organization, project, conversation, and generation boundaries; a
missing persisted anchor fails the review. Neighbors include at most eight tool calls with 4,000-rune arguments; truncation is explicit.
Serialized confirmation evidence is capped at 256 KiB; oversized requests are unavailable.
Body/tool evidence is bounded, and
truncated bodies are marked. Tool invocations retain names and arguments; stored
tool results retain their actor role, but historical rows may lack a tool name.

## Refusal fallback

Anthropic's cyber safety classifier can refuse a real injection payload
instead of judging it: the completion ends with `finish_reason: content_filter`
and no verdict. Without handling, every refusal would be an unavailable result,
so the attacks most likely to be dangerous would never become findings.

When Sonnet 5.5 refuses, the same evidence is sent once to Opus 4.8
(`anthropic/claude-opus-4.8`), Anthropic's recommended fallback for
cyber-category refusals. Its verdict is used as the confirmation, and the
result records the model that produced it. Both calls share the 45-second
confirmation deadline: fallback is best-effort within the remaining budget,
not a fresh 45-second attempt. A late primary refusal can leave too little time
for Opus 4.8; expiry or caller cancellation then produces an unavailable result.
The cascade remains bounded by 55 seconds overall, including Jev and context
loading. Shortening the primary deadline to reserve fallback time would turn
late refusals into timeouts, which do not trigger fallback under this policy.
If Opus 4.8 also refuses, the result is unavailable.
Only refusals fall back; timeouts, throttling, provider errors and malformed
responses do not. Refusals are recorded with the `refused` failure reason, and
the span's `pi_judge.refusal_fallback` attribute marks fallback verdicts.

Evidence from the 2,046-case benchmark (October 8): Sonnet 5.5 refused 8 of
Jev's 898 candidates, all attacks, and Opus 4.8 judged all 8 Not PI. Opus 5.5,
the earlier confirmer, refused 201 of the same candidates, so the fallback now
matters far less.

Only a confirmed verdict creates a finding. Jev errors, unavailable context,
malformed responses, provider throttling, and confirmer errors remain unavailable verdicts,
never completed clean scans. Existing delivery and finding persistence handle
retries; this change adds no topics, workflows, signals, activities, or schedules.
Additional Temporal actions/month: 0. Provider calls scale with scanned messages,
plus candidates meeting the threshold.

Concurrent cases are bounded, but PI calls do not use the shared Redis judge
rate limiter. Provider throttling remains an unavailable result; the benchmark
runs the same path without a Redis emulator.

The prefilter span records probability, escalation, token counts, and reported
provider cost without raw evidence. Physical-call metrics distinguish Jev and
the confirmer; the confirmer keeps the existing verdict/latency metrics. Existing finding APIs,
Platform MCP findings tools, and demo seed rows retain their
contracts; no new tool, permission, or seed shape is needed.

## Evaluation

Run `mise exec -- go run ./server/cmd/risk-pi-report -cascade` from the repository
root with `OPENROUTER_DEV_KEY` configured. The report uses production orchestration
and records confirmation calls, confirmation refusals, refusal-fallback calls,
prefilter misses, total provider cost, latency, precision, and recall. Run without
`-cascade` for the baseline. JSONL cases can
provide a `window` with up to five rendered messages and a `target_index`; cases
without a window evaluate the target with its trajectory. Both Jev and the confirmer
receive the same bounded trajectory used by the Gemini baseline. Conversation
windows supplement that trajectory when available; historical and live window
results must be reported separately.

`-sources cascade_context -check-floors=false` runs four synthetic conversation
smoke cases. These check transport and composition, not representative accuracy.
Evaluate the complete labeled corpus and real conversation examples to measure
precision, recall, and provider failures.

## Research and rollout evidence

[Evaluating Jev for Prompt Injection](https://claude.ai/artifact/BHwoQfUtzp87oMSvTpfekp)
records the flag-rate study, cost assumptions, labeled benchmark, and case
review.
The 0.50 cutoff matches the research baseline. In the enriched 10,000-message
sample it escalated 27 messages; 0.90 escalated none. The sample deliberately
included historical positives, so these are sample escalation rates, not natural
production prevalence or accuracy measurements.

### Confirmer and prompt

The report scores each option on 2,046 public cases with production payloads
against two goals: no false positives on the 1,066 benign cases, and at least
95% of the 175 well-known attacks caught. A well-known attack is a plain-text
attack with a classic phrase, such as ignore previous instructions, reveal the
system prompt, DAN or a fake system override.

| Option (prompt confirm-v4)      | False positives | Well-known caught | All attacks | Median decision |
| ------------------------------- | --------------- | ----------------- | ----------- | --------------- |
| Jev → Sonnet 5.5 (this cascade) | 0               | 169 of 175        | 82.4%       | 3.3 s           |
| Jev → Opus 5.5 → Opus 4.8       | 0               | 171 of 175        | 82.1%       | 4.9 s           |

Sonnet costs about $0.002 per confirmation against $0.003 for Opus 5.5, and
refuses far less. The confirm-v4 prompt treats role-play and "act as" requests
as content requests unless they also try to drop the agent's rules, reveal
protected data or send data out; personas with no rules (DAN, developer mode)
and discarding earlier instructions stay overrides. With the earlier prompt,
Sonnet flagged 2 relabelled deepset role-plays behind Jev.

## Platform MCP assessment

Outcome and resource: inspect stored prompt-injection findings through
`list_watchdog_findings`. The external actor is an authenticated organization
administrator; the tool also serves managed assistants under their existing
project scope. Its bounded, redacted rule-level alerts already represent the
findings produced by this scanner.

Decision: intentionally omit a Platform MCP tool change. Jev prefiltering and
Sonnet confirmation change internal classification, without adding a management
operation or changing finding schemas, authorization, project selection, or
redaction. The existing tool and the shipped
`summarize-critical-watchdog-findings` skill remain applicable; exposing raw
classification evidence or model controls would add an unnecessary internal
surface.

Success evidence: `TestCascadeConfirmedInjection` and
`TestCascadeConfirmationFailureIsUnavailable` cover confirmed findings and unavailable
reviews; `TestCascadeRefusalFallsBackToOpus48` and
`TestCascadeBothModelsRefusingIsUnavailable` cover the refusal fallback.
`TestRiskFindingsMCPInProcess`, `TestRiskFindingsEvidence`, and
`TestRiskFindingsValidationAndGates` cover the existing MCP result, redaction,
and access/feature boundaries. No Platform MCP schema or shipped workflow needs
to change for these internal classifier decisions.
