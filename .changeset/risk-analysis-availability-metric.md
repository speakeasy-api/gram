---
"server": patch
---

Report risk analysis availability on a single `risk.analysis.evaluations` counter spanning the prompt-injection judge, the prompt-policy judge, the fine-tuned risk analyzer and per-policy evaluation. Every risk engine fails open, so an outage previously looked identical to a quiet day; the counter separates evaluations that reached a verdict from those that produced nothing, with a bounded reason for the latter. OpenRouter failures keep their provider classification, so a drained credit balance, a revoked key, provider throttling and an upstream outage are no longer collapsed into one generic error. Datadog monitors are documented in `docs/runbooks/risk-analysis-availability-monitors.md`.
