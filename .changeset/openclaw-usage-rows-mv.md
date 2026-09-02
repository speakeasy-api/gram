---
"server": minor
---

OpenClaw sessions now contribute to the usage summaries. The per-turn usage and completed tool-call predicates in the attribute-metrics and chat-session materialized views previously counted only Codex, Cursor and OpenCode, so OpenClaw rows were ingested but skipped by every token, cost and tool-call aggregate. The change applies from deployment onward and does not retroactively aggregate existing OpenClaw sessions.
