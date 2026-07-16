---
"server": patch
---

Add the preset work-type taxonomy registry (DNO-540) that turn auto-categorization will classify against: a closed, versioned, two-level hierarchy (org function → activity) in `server/internal/worktype`, with stable persisted keys, display names, and per-category judge guidance describing the transcript signals that identify it. Parents are rollup-only; the judge picks from leaf categories plus the standalone Personal / Non-work and Other / Uncategorized buckets. No runtime behavior changes — nothing consumes the registry yet.
