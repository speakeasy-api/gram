---
"server": patch
---

Successful remote-session refreshes now backfill the RFC 8707 resource binding onto legacy rows that predate the resource column. The refresh CAS write stamps the resource the refresh grant actually used, but only when the stored value is NULL and the derivation is unambiguous — a stored binding is never overwritten, and an empty derivation leaves NULL in place. All refresh entry points (lazy MCP resolution, org-admin manual refresh, and the scheduled background sweep) converge through this write, so pre-column credentials now pick up a persisted routing key on their next refresh.
