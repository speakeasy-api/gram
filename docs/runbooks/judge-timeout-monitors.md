# Judge Timeout Monitors

Gram runs two OpenRouter-backed LLM judges inline on the message path:

- **Prompt-injection judge** (`server/internal/scanners/promptinjection/openrouter`) —
  L1 prompt-attack detector, emits `risk.prompt_injection.*` metrics.
- **LLM-as-a-judge** (`server/internal/scanners/promptpolicy/openrouter`) —
  operator-authored risk policy evaluator, emits `risk.judge.*` metrics.

Both cap a single completion call at a 10s client-side timeout (`judgeTimeout`)
and **fail open** on any error — a stuck model degrades to the L0 heuristics
rather than dropping the scan. That makes timeouts silent by default: the caller
just sees an allow. These monitors make them visible.

## Observability

Each judge records one counter and one duration histogram per call, tagged with
`outcome`:

| Judge            | Counter                                 | Duration histogram                     |
| ---------------- | --------------------------------------- | -------------------------------------- |
| Prompt injection | `risk.prompt_injection.classifications` | `risk.prompt_injection.judge_duration` |
| LLM-as-a-judge   | `risk.judge.evaluations`                | `risk.judge.duration`                  |

The `outcome` tag takes three values: `success`, `failure`, and — new — a
dedicated `timeout`. A deadline-exceeded or network timeout maps to
`outcome:timeout`; every other error stays `outcome:failure`, so timeouts are
alertable on their own rather than mixed in with socket hang-ups and other
undifferentiated failures. Fail-open warning logs also carry the `outcome` tag.

The duration histograms have an explicit `10` (seconds) bucket boundary aligned
to `judgeTimeout`, with finer resolution between 5s and 10s so you can watch
latency approach the timeout. Timed-out calls pile into the `(9, 10]` bucket.

## Datadog monitors

Create these in Datadog (monitors are managed in the Datadog UI, not in this
repo). Thresholds are starting points — tune against the production baseline.

### 1. Prompt-injection judge timeout rate

Alert when a meaningful fraction of prompt-injection judge calls time out.

```
Monitor type: Metric (ratio)
Query:  sum(last_5m):sum:risk.prompt_injection.classifications{outcome:timeout}.as_count()
      / sum:risk.prompt_injection.classifications{*}.as_count() * 100
Warn:  > 2     (% of calls)
Alert: > 5
```

### 2. LLM-as-a-judge timeout rate

```
Monitor type: Metric (ratio)
Query:  sum(last_5m):sum:risk.judge.evaluations{outcome:timeout}.as_count()
      / sum:risk.judge.evaluations{*}.as_count() * 100
Warn:  > 2
Alert: > 5
```

### 3. Judge p95 latency approaching the timeout

A leading indicator: p95 climbing toward 10s predicts a timeout spike before the
ratio monitors fire.

```
Monitor type: Metric
Query (prompt injection): p95:risk.prompt_injection.judge_duration{*}
Query (llm-as-a-judge):   p95:risk.judge.duration{*}
Warn:  > 7    (seconds)
Alert: > 9
```

Scope any of the above to a single tenant by adding an `organization_id` tag
filter when investigating a specific org.
