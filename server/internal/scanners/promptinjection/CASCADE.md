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

## Refusals

Anthropic's cyber safety classifier can refuse a real injection payload
instead of judging it: the completion ends with `finish_reason: content_filter`
and no verdict. A refusal is an unavailable result with the `refused` failure
reason, never a clean scan, and there is no second model to ask. On the
2,046-case benchmark (October 8), Sonnet 5.5 refused 8 of Jev's 898 candidates,
all attacks; Opus 5.5, the earlier confirmer, refused 201.

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

`server/cmd/risk-pi-report` scores the cascade this checkout ships, with production
orchestration and payloads: `mise exec -- go run ./server/cmd/risk-pi-report` from
the repository root with `OPENROUTER_DEV_KEY` configured. Each case's record holds
its outcome, the deciding model and its rationale, whether the confirmer refused,
cost, and latency. Case latency includes every whole-case attempt and retry wait,
from the first attempt until the final result. The gate measures recovered model
accuracy, not production availability within the relay's five-second budget; this
report neither enforces nor measures that budget. The baseline is main's own build,
which `mise run risk:pi` runs beside this change. JSONL cases can
provide a `window` with up to five rendered messages and a `target_index`; cases
without a window evaluate the target with its trajectory. Both Jev and the confirmer
receive the same bounded trajectory used by the Gemini baseline. Conversation
windows supplement that trajectory when available; historical and live window
results must be reported separately.

`mise exec -- go run ./server/cmd/risk-pi-report -corpus-dir
server/internal/scanners/promptinjection/testdata/cascade_smoke` runs four synthetic
conversation smoke cases. These check transport and composition, not representative
accuracy, so they live outside the scored corpus.
Evaluate the complete labeled corpus and real conversation examples to measure
precision, recall, and provider failures.

`mise run risk:pi` is the merge gate and the way to compare a change with main.
Run it locally before merging any change that can alter a verdict, and paste the
summary table it prints in the PR description. No CI job runs it, because it
calls paid models: a full run costs about $2.30 per side.

It runs the cascade for this change and for main (the merge-base with
`origin/main`, built from a detached worktree) on the 2,046 cases the evaluation
report scored, deepset included. This
change fails unless no benign case is flagged and at least 80% of all attacks
are caught (780 of 975). A unit test pins the corpus size and the 169 reviewed fixtures
tagged `well_known`. Before scoring, a refused or malformed confirmation is
asked again, up to three calls as the report's harness did, and a case that
failed open on throttling, a server error or a timeout runs again after 5, 10
and 20 seconds. A confirmation still refused is a miss.

Each side runs its own commit's build of the report, which writes each case's
result to `~/.cache/gram-pi-eval/runs/<code key>` as it finishes. The code key
hashes the commit's code outside the fixtures, so commits that change only
fixtures share a run. A rerun resumes, runs only new or edited fixtures, and
redoes cases a provider rejected for lack of credit once the balance covers them.
`--watch` serves a live viewer during the run, and `--view` and `--summary-md`
read cached results. The viewer lists every cached run and compares any two, by
default main and this change, case by case.

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
against two goals: no false positives on the 1,071 benign cases, and at least
95% of the 175 well-known attacks caught. A well-known attack is a plain-text
attack with a classic phrase, such as ignore previous instructions, reveal the
system prompt, DAN or a fake system override.

| Option (prompt confirm-v4)      | False positives | Well-known caught | All attacks | Median decision |
| ------------------------------- | --------------- | ----------------- | ----------- | --------------- |
| Jev → Sonnet 5.5 (this cascade) | 0               | 169 of 175        | 82.9%       | 3.3 s           |
| Jev → Opus 5.5 → Opus 4.8       | 0               | 171 of 175        | 82.6%       | 4.9 s           |

Sonnet costs about $0.002 per confirmation against $0.003 for Opus 5.5, and
refuses far less. The current prompt clarifies confirm-v4: role-play and "act as"
requests remain content requests unless the evidence shows they displace higher-priority
runtime rules or the authorized user's intent, including through role or priority
changes. Explicit override wording is not required for a role-play directive
planted in untrusted content to redirect the agent away from the user's task.
Personas with no rules (DAN, developer mode) and discarding earlier instructions
stay overrides. The paired examples distinguish authorized user role-play from
the same directive planted in a tool result. This clarification has not been
benchmarked; the table above reports confirm-v4 before the clarification. With
the earlier prompt, Sonnet flagged 2 relabelled deepset role-plays behind Jev.

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
reviews; `TestCascadeConfirmationRefusalIsUnavailable` covers refusals.
`TestRiskFindingsMCPInProcess`, `TestRiskFindingsEvidence`, and
`TestRiskFindingsValidationAndGates` cover the existing MCP result, redaction,
and access/feature boundaries. No Platform MCP schema or shipped workflow needs
to change for these internal classifier decisions.
