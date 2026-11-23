---
"server": patch
---

Risk analysis now reports whether it is actually running. A new `risk.analysis.evaluations` counter records each evaluation of the prompt-injection judge, the prompt-policy judge, the LLM analyzer and each customer policy as completed, degraded or canceled, with a bounded reason such as `insufficient_credits`, `rate_limited`, `upstream_unavailable`, `not_configured` or `dependency_unavailable`. Because every engine fails open, an outage previously looked like a quiet day on every other risk metric. The runbook in `docs/runbooks/risk-analysis-availability-monitors.md` lists the Datadog monitors to create on it.
