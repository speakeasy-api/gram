---
"server": patch
---

Add a nullable `match_config` JSONB column to `risk_custom_detection_rules`.
Detection rules will evaluate this structured condition config instead of the
single `regex` pattern; `regex` is retained (nullable) as a fallback until a
later backfill+contract migration. Schema-only.
