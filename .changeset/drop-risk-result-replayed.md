---
"server": minor
"dashboard": patch
---

Remove the unused `replayed` field from the `RiskResult` API type. The flag was
denormalized from the scanned chat message onto every risk listing row but never
rendered by any consumer; dropping it shrinks the listing queries ahead of
serving the Risk Events page from ClickHouse.
