---
"server": minor
---

Meter OTEL-invisible Codex cloud surfaces from the compliance COSTS feed
(DNO-751). Codex rows whose payload.client is on the cloud allowlist (github,
web) now promote their preserved codex.compliance.\*\_tokens counts to
gen_ai.usage.\* and count toward usage metering and TUM — these surfaces
(GitHub code review, Codex web tasks) have no OTEL stream, so the compliance
feed is their only token source. Device clients (cli, exec) stay cost-only
(OTEL remains their token source of truth) and ambiguous clients
(desktop_app, unknown) stay un-metered until desktop OTEL coverage is
verified; the allowlist means new surfaces default to un-metered rather than
double counting.
