# Prompt-policy cascade

The `risk-prompt-policy-cascade` PostHog flag selects the cascade by organization/project group keys. Create it disabled before merging, then target organizations for evaluation. Missing, disabled, or unavailable local flag evaluations retain the baseline judge.

Jev screens the target against the policy. A violation probability below 0.90 completes without a finding. At or above 0.90, Opus reviews the target with up to five conversation messages; only its confirmed violation creates a finding. Persisted targets use two neighbors on each side. Live targets with a known chat use the four latest persisted messages. Unlinked targets have no invented context. Provider and context failures report no verdict, including for policies with legacy fail-closed configuration.

The cascade trace records prefilter probability, cost, token usage, latency, escalation, context coverage, and final confirmation. The existing judge span and metrics cover the Opus completion. Successful verdicts include both stages' provider costs and tokens.

## Synthetic comparison

Run the six synthetic cases through the production baseline and cascade with a development OpenRouter key available in the environment:

```sh
mise run test:server -tags=policy_cascade_eval ./internal/scanners/promptpolicy/openrouter -run '^TestCascadeLiveEvaluation$' -v -count=1
```

This opt-in test makes paid provider calls and reports false positives, misses, escalation, latency, and cost. The normal unit suite verifies threshold boundaries, independent rejection, context payloads, rollout fallback, and provider failures without network access. The final six-case run produced baseline TP=3, FP=1, TN=2, FN=0 and cascade TP=2, FP=0, TN=3, FN=1, with two escalations and no provider failures. An earlier run filtered one additional positive below the Jev threshold. The context-dependent deletion case was missed in both runs; a secondary judge cannot recover prefilter misses. These cases are a smoke comparison, not evidence of production precision or recall; evaluate representative labeled traffic before rollout.

Platform MCP: `list_watchdog_findings`, `list_risk_policies`, and `get_risk_policy` retain their existing resources, actors, authorization, and result schemas. This internal judging change needs no new tool. Existing demo findings already exercise the unchanged dashboard data shape.
