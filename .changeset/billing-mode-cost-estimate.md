---
"dashboard": patch
---

Cost is now shown as an estimate ("Est. cost", with an explanation on hover) wherever it appears in Costs and insights, because the figure is derived from token usage at standard API rates — which only reflects real spend on metered (pay-per-token) accounts, not flat-fee subscription plans like Claude Max/Pro/Team/Enterprise. Admins can declare a provider integration's billing mode (Metered / Flat rate / Unknown) under Settings → Logging & Telemetry; once an account is declared metered, its cost reads as a confident "Cost".
