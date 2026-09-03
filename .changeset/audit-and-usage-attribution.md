---
"server": patch
---

Report per-user usage and audit actors accurately. The per-user metrics summary now counts Claude Code and Codex usage, which report tokens and cost on their own attributes rather than the generic `gen_ai.usage.*` path it read — someone working through either surface showed zero tokens and zero spend while the cost dashboard billed them in full. Audit log actors resolve against the directory at read time, in the feed, its actor filter and the admin activity list alike: every writer stores the acting user's email, so all three read as columns of addresses.
