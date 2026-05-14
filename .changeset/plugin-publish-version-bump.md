---
"server": patch
---

Fix plugin re-publish so Claude Code, Cursor, and Codex marketplace clients refresh installed copies. Every plugin manifest now ships with a per-publish version (`0.1.<unix_ts>`) instead of a hardcoded `0.1.0`, so platform clients see a newer version on republish and pull the updated content.
