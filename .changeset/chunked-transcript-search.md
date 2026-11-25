---
"server": minor
"dashboard": minor
---

Searching a conversation in a project that keeps encrypted transcripts now scans it in chunks. `chat.load` with `query` accepts `scan_cursor` and returns `scan_has_more`, `next_scan_cursor` and `scanned_messages`, and the dashboard shows each chunk's matches as they arrive with the scan's progress. The Platform MCP `search_chats` tool scans a bounded number of messages per call in those projects and returns `scan_incomplete` with a note, so the agent decides whether to continue with `next_cursor`. Other projects are searched in one request, as before.
