---
"server": minor
---

A prompt policy's judge timeout or error during enforcement now follows the organization's risk failure mode, the new org-admin `risk_enforcement_fail_closed` product feature, instead of each policy's `fail_open` setting. Hooks and MCP enforcement block the message while it is on and allow it while it is off, the default. The guardrail replay shows the same verdict. Background analysis that only flags never records a judge outage as a finding. `RiskPolicyModelConfig.fail_open` is deprecated: the API accepts and ignores it, no longer stores it, and no longer returns it. A `fail_open` value already stored on a policy is ignored. The `risk-fail-closed` operator backfill turns the setting on for every organization with a prompt policy that stored `fail_open: false`.
