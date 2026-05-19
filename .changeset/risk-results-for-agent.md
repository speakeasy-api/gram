---
"server": minor
"dashboard": minor
---

Adds `risk.results.listForAgent` — a redacted variant of `risk.results.list` for AI assistant / MCP consumption. The new endpoint returns the same fields as `listRiskResults` but replaces the `match` field with `match_redacted`, an opaque token of the form `<redacted len=N sha=XXXXXXXX>` where `N` is the byte length and `XXXXXXXX` is the first 8 hex characters of `sha256(match)`. Identical secrets produce identical fingerprints so agents can dedupe leak counts without ever seeing secret content.

`shadow_mcp` findings pass `match` through verbatim because the value is a server URL or stdio command identifier (already shown unmasked in the dashboard), and exact byte positions are coarsened to a single `position_known` boolean to remove reconstruction signals.

The dashboard's AI Insights sidebar gains risk-aware suggestions on the Security Overview and Policy Center pages, plus a system-prompt rule that bars the assistant from echoing `match_redacted` values verbatim.
