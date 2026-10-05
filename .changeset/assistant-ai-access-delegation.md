---
"server": minor
---

Enforce the internal `ai_access` kill switch for assistant work started from a validated dashboard session. Those turns carry a short-lived current-user delegation, revalidated against active membership on every model and MCP call and gated by the MCP kill switch rollout flags. Triggered runs (Slack, Teams, Linear, GitHub, cron, wake), MCP authorization resumption, and messages sent without a validated session keep the assistant-only behavior.
