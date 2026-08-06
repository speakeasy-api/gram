---
"server": minor
"dashboard": patch
---

Classify Codex account identity and billing mode (DNO-734). Codex sessions on
every capture path (legacy hooks, OTEL logs, ingest adapter) now stamp
account_type from email resolution — resolved work email is team, anything
else personal — and team sessions resolve the org-level billing mode declared
on the codex_compliance integration config (the session provider "openai" now
maps to that config, fixing the mapping bug that made the config's
billing_mode unreachable). Compliance COSTS import rows (codex and
ChatGPT/Work) carry account_type=team and the config's billing mode directly.
The estimated-cost tooltip copy mentions ChatGPT plans alongside Claude's.
