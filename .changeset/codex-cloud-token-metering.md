---
"server": minor
---

Meter Codex cloud usage (GitHub code review, web tasks) from the compliance
COSTS feed — those surfaces have no OTEL stream, so their token counts now
promote to `gen_ai.usage.*` and count toward TUM. Device clients keep
metering via OTEL, and unrecognized clients stay un-metered so a new surface
cannot silently double count.
