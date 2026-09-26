# Jev prompt-policy evaluation

The `risk-prompt-policy-jev` PostHog flag selects direct Jev policy evaluation by organization/project group keys. Create it disabled before merging, then target organizations for evaluation. Missing, disabled, or unavailable local flag evaluations retain the baseline judge.

Jev evaluates the target message against the operator-authored policy. A violation probability of at least 0.90 creates a finding directly; a lower probability completes without a finding. No secondary model or conversation retrieval runs when Jev is enabled. Provider failures and invalid probabilities report no verdict, including for policies with legacy fail-closed configuration.

The Jev trace records probability, cost, token usage, latency, and match outcome. Existing judge metrics record evaluation duration, provider errors, and matched probabilities. Completed verdicts carry Jev model attribution and usage for the existing metering paths. The probability threshold favors fewer false positives; representative labeled evaluation is required before rollout.

Platform MCP: `list_watchdog_findings`, `list_risk_policies`, and `get_risk_policy` retain their existing resources, actors, authorization, and result schemas. This internal judging change needs no new tool. Existing demo findings already exercise the unchanged dashboard data shape.
