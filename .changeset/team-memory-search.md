---
"server": patch
---

The Platform MCP has a new `search_team_memory` tool for organizations with Organizational Memory turned on in the staff admin dashboard. It searches one project's Organizational Memory, best match first, and covers masked session summaries, saved memos, and day, week and month digests. Every hit carries the source to cite. Sessions that are excluded from memory, deleted or from personal accounts never appear, and neither do memos saved from them. It needs `memory:read` on the project (organization admins have it by default, members need a grant), and every search is audited.
