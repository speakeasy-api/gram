---
"server": patch
---

The Platform MCP has two new Organizational Memory tools. `recall_team_history` returns one project's last year of memory as a single block, oldest first: month digests for older history, then week and day digests not yet rolled up, then the latest session summaries and memos. Each section carries a citation, and the oldest sections are dropped first to stay within the size limit (4,000 estimated tokens by default, up to 8,000). `zoom` opens a cited item one level down: a month or week digest shows the digests it rolled up, a day digest shows the session summaries and memos it was written from, and a summary or memo shows its full text. Both need `memory:read` on the project, leave out sessions that are excluded, deleted or from personal accounts, mask secrets before returning text, and are audited.
