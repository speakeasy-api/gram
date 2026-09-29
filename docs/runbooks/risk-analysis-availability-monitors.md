# Risk Analysis Availability Monitors

Every risk engine in Gram **fails open**. A prompt-injection judge that cannot
reach OpenRouter returns `UNAVAILABLE` and the message is allowed through; a
customer policy whose evaluation errors is logged at warn and skipped; the
fine-tuned analyzer turns a model outage into a dead-letter sentinel. That is
the right runtime behaviour — an analysis outage must not break the product —
but it also means a total outage looks exactly like a quiet day on every
metric that counts findings, blocks or scans.

Prompt-injection scanning once sat offline on a drained OpenRouter credit
balance for that reason, and nothing paged. These monitors exist so that
cannot happen silently again.

## What to alert on

There are three distinct questions, and they need three different monitors:

1. **Is risk analysis running at all?** Answered by a no-data monitor on
   `risk.analysis.evaluations`.
2. **Is a meaningful share of it failing?** Answered by a ratio of the
   `degraded` outcome to all non-canceled evaluations.
3. **Is it failing for a reason that never self-heals?** Answered by a
   threshold on the specific `insufficient_credits`, `unauthorized` and
   `key_disabled` reasons, which stay broken until a human acts.

## Metric contract

`risk.analysis.evaluations` is a counter (`{evaluation}`) emitted by
`server/internal/riskhealth`. It is a wire contract: monitors below are
written against these exact names and values, and
`server/internal/riskhealth/riskhealth_test.go` pins them.

| Tag                            | Meaning                                                |
| ------------------------------ | ------------------------------------------------------ |
| `gram.org.id`                  | Organization whose traffic was being analyzed.          |
| `gram.risk.component`          | Which part of risk analysis ran.                        |
| `gram.outcome`                 | What the evaluation produced.                           |
| `gram.risk.degradation_reason` | Why it produced nothing. `none` unless `degraded`.      |

`gram.risk.component`:

- `prompt_injection_judge` — the OpenRouter-backed prompt-injection judge, on
  both the realtime and batch paths.
- `prompt_policy_judge` — the OpenRouter-backed judge behind customer-authored
  prompt policies.
- `llm_analyzer` — the self-hosted fine-tuned risk model (`GRAM_RISK_LLM_URL`),
  emitted from the `gram streams` process.
- `policy_evaluation` — one customer risk policy evaluated against one event
  in realtime enforcement, whatever engines it used.

`gram.outcome`:

- `completed` — an engine reached a usable verdict. A clean verdict and a
  finding are both `completed`.
- `degraded` — no usable verdict, so the event went unanalyzed. **This is the
  alertable outcome.**
- `canceled` — the caller abandoned the evaluation. Deliberately excluded from
  the ratios below: a deploy draining in-flight requests is not an outage.

`gram.risk.degradation_reason`:

| Reason                  | Self-heals? | What it means                                                     |
| ----------------------- | ----------- | ----------------------------------------------------------------- |
| `insufficient_credits`  | No          | The model provider balance is drained (OpenRouter 402).            |
| `unauthorized`          | No          | Missing, revoked or unentitled provider key.                       |
| `key_disabled`          | No          | Gram locked down its own provisioned provider key.                 |
| `not_configured`        | No          | The engine is not wired up in this deployment at all.              |
| `rate_limited`          | Maybe       | The model provider throttled Gram.                                 |
| `throttled`             | Maybe       | Gram's own per-organization judge limiter refused the call.        |
| `upstream_unavailable`  | Usually     | Provider 5xx, edge timeout or overload.                            |
| `timeout`               | Usually     | The evaluation ran out of time.                                    |
| `malformed_response`    | Usually     | The model answered with something that is not a verdict.           |
| `dependency_unavailable`| Usually     | A policy reached no verdict because an engine it needs was down.   |
| `policy_error`          | No          | A customer policy errored during its own evaluation.               |
| `bad_request`           | No          | The provider rejected what Gram sent. An engineering signal.       |
| `error`                 | Unknown     | Unclassified.                                                      |

A provider content-policy refusal is **not** degraded: the provider looked at
the payload and declined it, which is a verdict about the content rather than
an analysis outage.

### Related per-engine metrics

This counter is deliberately coarse. Once a monitor fires, the per-engine
metrics carry the detail:

- `risk.prompt_injection.typed_fail_open_samples` — the PI judge's own
  fail-open breakdown. Its `reason` tag carries the same provider
  classification (`insufficient_credits`, `rate_limited`,
  `upstream_unavailable`, `unauthorized`, …).
- `risk.llm.requests` / `risk.llm.duration` — the fine-tuned analyzer's
  transport outcomes.
- `risk.judge.evaluations` / `risk.judge.duration` — the prompt-policy judge's
  latency and outcome. See `judge-timeout-monitors.md`.
- `risk.enforcement.pubsub_degraded` — Pub/Sub enforcement lanes, including
  whether the scan then failed open or closed.
- `openrouter.credits.*` — the credit balance gauges collected by the
  `collect-openrouter-credits-metrics` Temporal schedule. A credit monitor on
  those gauges is a **leading** indicator; the one below is the confirmation
  that scanning has already stopped.

## Datadog monitors

Monitors are managed in the Datadog UI, not in this repository. Scope every
query to `service:gram-server` unless noted, and link the notification back to
this runbook. Thresholds are starting points — tune against the production
baseline before enabling paging.

### 1. Risk analysis stopped reporting (no data)

The catch-all. It fires when the counter goes silent, which covers an engine
that was never configured, a worker that is not running, and a deploy that
dropped the instrumentation.

```text
Monitor type: Metric (no data)
Query:  sum(last_15m):sum:risk.analysis.evaluations{service:gram-server}.as_count()
Notify: no data after 30m
```

Split by `gram.risk.component` once every component has a steady baseline, so
one engine going dark does not hide behind the others. Do not split by
`gram.org.id`: organizations with genuinely quiet traffic would alert
constantly.

### 2. Degraded evaluation ratio

The main health monitor. Cancellation is excluded from both sides so a deploy
does not move it.

```text
Monitor type: Metric (ratio)
Query:  sum(last_10m):sum:risk.analysis.evaluations{service:gram-server,gram.outcome:degraded}.as_count()
      / sum:risk.analysis.evaluations{service:gram-server,NOT gram.outcome:canceled}.as_count() * 100
Warn:  > 5     (% of evaluations)
Alert: > 20
```

Once it fires, break the numerator down by `gram.risk.degradation_reason` and
`gram.risk.component` to pick the response below.

### 3. Risk analysis blocked on a credential or billing problem

These reasons never recover on their own, so any sustained volume is
actionable regardless of ratio. This is the monitor that would have caught the
credit exhaustion.

```text
Monitor type: Metric
Query:  sum(last_15m):sum:risk.analysis.evaluations{
          service:gram-server,
          gram.risk.degradation_reason IN (insufficient_credits, unauthorized, key_disabled, not_configured)
        }.as_count()
Warn:  > 0
Alert: > 25
```

Group by `gram.risk.degradation_reason` and `gram.risk.component` so the
notification names the fix: top up the OpenRouter balance, restore the key, or
set the missing configuration.

### 4. Model provider outage

Separated from monitor 3 because the response is different: wait and watch
rather than act on billing or credentials.

```text
Monitor type: Metric (ratio)
Query:  sum(last_10m):sum:risk.analysis.evaluations{
          service:gram-server,
          gram.risk.degradation_reason IN (upstream_unavailable, rate_limited, timeout)
        }.as_count()
      / sum:risk.analysis.evaluations{service:gram-server,NOT gram.outcome:canceled}.as_count() * 100
Warn:  > 10
Alert: > 30
```

### 5. Customer policies erroring or no-oping

The per-customer half of the signal: a policy the customer enabled is not
producing verdicts. `policy_error` is a fault inside the policy's own
evaluation; `dependency_unavailable` is a policy that reached no verdict
because an engine it depends on was down.

```text
Monitor type: Metric (grouped)
Query:  sum(last_30m):sum:risk.analysis.evaluations{
          service:gram-server,
          gram.risk.degradation_reason IN (policy_error, dependency_unavailable)
        } by {gram.org.id}.as_count()
Warn:  > 10
Alert: > 100
```

Group by `gram.org.id` so the alert names the affected customer. `policy_error`
for a single organization usually means that customer's policy configuration,
not a platform outage — reproduce with their policy before escalating.

### 6. Prompt-policy judge unconfigured

A deployment serving prompt-based policies with no judge wired evaluates
nothing and reports nothing wrong to the customer. It is worth its own
zero-tolerance monitor because it is a configuration mistake, not a failure.

```text
Monitor type: Metric
Query:  sum(last_15m):sum:risk.analysis.evaluations{
          service:gram-server,
          gram.risk.component:prompt_policy_judge,
          gram.risk.degradation_reason:not_configured
        }.as_count()
Alert: > 0
```

## Response

| Reason                                      | First action                                                                                 |
| ------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `insufficient_credits`                      | Top up the OpenRouter balance. Cross-check the `openrouter.credits.*` gauges for the trend.    |
| `unauthorized`, `key_disabled`              | Check the provider key: revoked upstream, or disabled by Gram (see `disable_causes.go`).       |
| `not_configured`                            | Check the deployment's model configuration (`GRAM_RISK_LLM_URL`, the OpenRouter client wiring). |
| `rate_limited`                              | Check the provider quota; consider raising the per-org judge limit.                            |
| `throttled`                                 | Gram's own limiter. Raise the judge rate limit or shed load.                                   |
| `upstream_unavailable`, `timeout`           | Check the provider status page and the per-engine latency histograms.                          |
| `malformed_response`                        | A model or prompt regression. Check for a recent model or schema change.                       |
| `policy_error`, `dependency_unavailable`    | Identify the organization, then read the warn logs carrying `gram.risk.policy_id`.             |

While any of these is firing, assume content is passing through **unanalyzed**
for the affected component. The one exception is the LLM analyzer in the `llm`
engine mode, which fails closed: there the symptom is denied traffic, not
missed detections.

## Ownership

- **Owner:** Risk / Guardrails on-call
- **Services:** `gram-server`, `gram-streams`
- **Emitting package:** `server/internal/riskhealth`
