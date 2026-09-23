---
"server": patch
---

The `gram-risk-llm-analyzer` feature flag now selects a risk engine mode per organization: `off` keeps the gitleaks, Presidio, prompt-injection and destructive-tool engines; `shadow` keeps those engines enforcing while the fine-tuned risk model also scans the same traffic so its verdicts can be compared, never denying a request on its own; `llm` swaps the engines for the model as before. Organizations on the existing boolean flag keep today's behaviour until the flag is switched to multivariate in PostHog.
