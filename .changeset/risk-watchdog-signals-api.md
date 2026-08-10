---
"server": minor
---

Add `risk.getSignals`, the ClickHouse-only endpoint powering the Watchdog page: findings clustered by rule into ranked "signals" with heuristic severity scores on the existing 0.1-10 scale, window-level KPI splits (24h findings, users exposed, open/critical signals), an exposure-by-category rollup, per-signal sparkline series, and top affected users. The five ClickHouse reads fan out concurrently. Finding ingest now denormalizes the scanned message's canonical source app (`chat_source`), the user's WorkOS directory department (`team`), and the resolved user's email (`user_email`) so the endpoint needs no Postgres lookups; the local seed stamps deterministic app/team/email attribution, embeds finding matches into anchor message content with reveal spans, and mirrors reveal metadata into ClickHouse so the reveal path works end to end on seeded data.
