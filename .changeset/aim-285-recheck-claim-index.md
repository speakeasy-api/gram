---
"server": patch
---

Add a partial index on `remote_sessions` covering the keepalive re-check population (no refresh token, no refresh expiry) ordered by its due clock, so the re-check claim query walks the index instead of sequentially scanning the table on every tick.
