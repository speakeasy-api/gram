# Risk Analysis Availability Monitors

Every risk engine fails open or degrades to a sentinel. A prompt-injection
judge that cannot reach OpenRouter returns "unavailable" and the message goes
through. A fail-open prompt policy whose judge errors returns no finding. The
LLM analyzer turns a model outage into a dead-letter sentinel. That is the
right runtime behavior, but it means an outage looks exactly like a quiet day on
every finding, block and scan metric. Prompt-injection scanning once sat dark on
a drained OpenRouter credit balance for that reason, and nothing paged.

`risk.analysis.evaluations` answers "is risk analysis actually running?" across
every engine and every customer policy. These monitors page on it.

## Current deployment state

The metric is implemented and covered by tests. The monitors below are **not
verified as deployed**. Create them in Datadog (monitors are managed in the
Datadog UI, not in this repository) and link each notification to this
runbook.

## Metric contract

One counter, `risk.analysis.evaluations`, emitted by `gram-server`,
`gram-streams` and `gram-worker`. Scope monitors by `env`, not by `service`:
the LLM analyzer only runs in `gram-streams` and batch prompt-injection scans
run in the worker, so a single-service query silently drops them.

| Tag                                     | Values                                                                               |
| --------------------------------------- | ------------------------------------------------------------------------------------ |
| `gram.risk.analysis.component`          | `prompt_injection_judge`, `prompt_policy_judge`, `llm_analyzer`, `policy_evaluation` |
| `gram.outcome`                          | `completed`, `degraded`, `canceled`                                                  |
| `gram.risk.analysis.degradation_reason` | `none` on completed and canceled samples, otherwise one of the reasons below         |
| `gram.org.id`                           | the organization the evaluated event belongs to                                      |
| `gram.risk.policy_id`                   | only on `policy_evaluation`: the customer policy evaluated                           |

Components:

- `prompt_injection_judge`: one message classified by the prompt-injection
  judge, including the prefilter when it runs as a cascade.
- `prompt_policy_judge`: one message judged against one customer prompt
  policy.
- `llm_analyzer`: one message analyzed by the fine-tuned risk model.
- `policy_evaluation`: one customer policy evaluated against one realtime or MCP
  event, across every engine it depends on. This is the layer that catches a
  policy which silently stopped protecting the customer, including engines
  with no component of their own (Presidio, gitleaks, custom rules).

`canceled` means the caller abandoned the evaluation: the request ended, a
sibling policy already blocked, or the process is draining. It is excluded from
every ratio below so deploys and normal fan-out do not page.

## Degradation reasons and first response

| Reason                   | Meaning                                                                             | First action                                                                                                  |
| ------------------------ | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `insufficient_credits`   | Provider returned 402: the key's credit balance or limit is exhausted.              | Raise the key's credit limit in OpenRouter (or the model host). Does not clear on its own.                    |
| `key_disabled`           | The organization's provisioned platform key is locked down.                         | Check why the key was disabled; re-enable or reprovision it.                                                  |
| `unauthorized`           | Provider rejected the credentials (401, or a 403 that is not a content refusal).    | Check the key is valid and entitled to the model.                                                             |
| `rate_limited`           | Provider throttled the call (429).                                                  | Check OpenRouter or model-host rate limits and upstream capacity.                                             |
| `throttled`              | Speakeasy's own per-organization judge limiter refused the call.                    | Expected during bursts from one organization; investigate only if sustained.                                  |
| `upstream_unavailable`   | Provider or model host answered 5xx or overloaded.                                  | Check provider status; for the LLM analyzer, check the model deployment.                                      |
| `timeout`                | The call ran past its deadline.                                                     | Check judge latency (`risk.prompt_injection.judge_duration`, `risk.judge.duration`, `risk.llm.duration`).     |
| `content_policy`         | Provider's safety classifier refused the payload.                                   | Usually payload-specific; investigate if it spikes for one organization.                                      |
| `bad_request`            | Provider rejected the request as invalid (unknown model, unsupported parameter).    | A deploy or provider change broke the request shape; check recent changes to the judge models and parameters. |
| `malformed_response`     | The model answered but its reply was not a verdict.                                 | Check for a model or prompt change.                                                                           |
| `truncated_response`     | The reply hit the completion token cap.                                             | Check `risk.prompt_injection.typed_completion_tokens`; consider raising the cap.                              |
| `input_too_large`        | The evidence exceeded what the engine can judge, so it declined.                    | Expected baseline for very large events; excluded from the ratio monitor.                                     |
| `not_configured`         | No engine is wired for the evaluation in this deployment.                           | A deployment is missing configuration (for example `GRAM_RISK_LLM_URL` or the typesafe decisions endpoint).   |
| `dependency_unavailable` | A customer policy reached no verdict because an engine it depends on degraded.      | Find the degraded engine on the same org and time window by component and reason.                             |
| `policy_error`           | A customer policy failed outside its engines (reading exclusions, compiling scope). | A bug or bad policy data; check `risk-scanner` warn logs for the policy id.                                   |
| `error`                  | Anything else.                                                                      | Check the engine's warn logs for the same window.                                                             |

The per-engine metrics remain the drill-down once a monitor fires:
`risk.prompt_injection.typed_fail_open_samples{reason}`,
`risk.prompt_injection.typed_physical_calls{failure_reason}`,
`risk.judge.evaluations{gram.outcome}`, `risk.llm.requests{gram.outcome}` and
`risk.enforcement.pubsub_degraded{reason}`.

## Datadog monitors

Thresholds are starting points; tune them against the production baseline.
Replace `<ENV>` with the environment tag value.

### 1. Credential or billing failure

These never clear on their own, so page on any sustained occurrence. This is
the monitor that would have caught the drained credit balance.

```
Monitor type: Metric (multi alert by gram.risk.analysis.component, gram.risk.analysis.degradation_reason)
Query:  sum(last_10m):sum:risk.analysis.evaluations{env:<ENV>,gram.outcome:degraded,gram.risk.analysis.degradation_reason IN (insufficient_credits,key_disabled,unauthorized)} by {gram.risk.analysis.component,gram.risk.analysis.degradation_reason}.as_count() > 5
Warn:  > 0
Alert: > 5
Priority: P1
```

### 2. Engine degraded ratio

Catches provider outages, rate limiting, timeouts and anything else that makes a
component stop reaching verdicts.

```
Monitor type: Metric (multi alert by gram.risk.analysis.component)
Query:  sum(last_15m):(
          sum:risk.analysis.evaluations{env:<ENV>,gram.outcome:degraded,!gram.risk.analysis.degradation_reason:input_too_large,!gram.risk.analysis.degradation_reason:throttled} by {gram.risk.analysis.component}.as_count()
        / sum:risk.analysis.evaluations{env:<ENV>,!gram.outcome:canceled} by {gram.risk.analysis.component}.as_count()
        ) * 100 > 20
Warn:  > 5     (% of evaluations)
Alert: > 20
Priority: P2
```

### 3. Risk analysis stopped

A component that records nothing at all is either idle or broken before it
could record. Enable "Notify if data is missing" so a process that stopped
calling the engines alerts too. Scope it to the components that run
continuously in the environment.

```
Monitor type: Metric (multi alert by gram.risk.analysis.component)
Query:  sum(last_30m):sum:risk.analysis.evaluations{env:<ENV>,gram.outcome:completed} by {gram.risk.analysis.component}.as_count() < 1
Notify if data is missing: after 30 minutes
Priority: P2
```

### 4. Customer policy not protecting

Per customer policy: the policy keeps running but reaches no verdict. This is
the "customer policy errors or silently no-ops" case. Route it to the team that
owns customer escalations as well as on-call.

```
Monitor type: Metric (multi alert by gram.org.id, gram.risk.policy_id)
Query:  sum(last_1h):sum:risk.analysis.evaluations{env:<ENV>,gram.risk.analysis.component:policy_evaluation,gram.outcome:degraded} by {gram.org.id,gram.risk.policy_id}.as_count() > 20
Warn:  > 5
Alert: > 20
Priority: P2
```

### 5. Customer policy errors

`policy_error` is deterministic (a bug or bad policy data), so any sustained
occurrence is worth a look.

```
Monitor type: Metric (multi alert by gram.org.id, gram.risk.policy_id)
Query:  sum(last_30m):sum:risk.analysis.evaluations{env:<ENV>,gram.risk.analysis.component:policy_evaluation,gram.risk.analysis.degradation_reason:policy_error} by {gram.org.id,gram.risk.policy_id}.as_count() > 0
Priority: P3
```

### 6. Unconfigured engine

An engine evaluated with nothing wired behind it: a deployment is missing
configuration.

```
Monitor type: Metric (multi alert by gram.risk.analysis.component)
Query:  sum(last_30m):sum:risk.analysis.evaluations{env:<ENV>,gram.risk.analysis.degradation_reason:not_configured} by {gram.risk.analysis.component}.as_count() > 0
Priority: P3
```

## Notification

Each monitor message should name the component and reason from the group tags,
link this runbook, and notify the risk team's on-call channel. Incidents from
monitors 1 and 2 affect every customer with risk policies, so declare an
incident rather than posting in a channel.
