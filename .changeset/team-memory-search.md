---
"server": patch
---

The Platform MCP has a new `search_team_memory` tool, behind the `team-memory` flag. It searches one project's team memory, best match first, and covers masked session summaries, saved memos, and day, week and month digests. Every hit carries the source to cite. Sessions that are excluded from memory, deleted or from personal accounts never appear, and neither do memos saved from them. It needs an explicit `memory:read` grant on the project, and every search is audited.
