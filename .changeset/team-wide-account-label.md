---
"dashboard": patch
---

Label the empty user bucket on cost breakdowns as "Team-wide account" instead of "(unset)". Claude Code sessions authenticated with a company API key or gateway emit no user identity, so their pooled spend is the shared team account's usage, not a data gap. The label applies everywhere the user dimension renders — the cost table, the Top spenders widget (which now includes the bucket instead of hiding it), the drill-in profile, breadcrumbs, and the billing token-usage breakdown — while other dimensions keep "(unset)".
