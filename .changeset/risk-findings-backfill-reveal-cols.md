---
"server": patch
---

Rework the risk_findings backfill tool to emit complete reveal metadata: one
ClickHouse row per recorded finding span with surface/field/path attribution,
content-part anchors, message event time and assistant attribution, and the
new shared partial-mask match display (domain-only emails, last-4 financial,
boundary-character tiers) that the Risk Events listing renders by default.
