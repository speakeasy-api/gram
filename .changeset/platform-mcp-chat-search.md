---
"server": patch
---

Add `search_chats` and `read_chat` to Platform MCP so members with `chat:read` can search captured sessions, commands, and tool inputs/results across historical dates and inspect redacted message context. Results use bounded pagination, enforce exact project access, and audit session reads.
