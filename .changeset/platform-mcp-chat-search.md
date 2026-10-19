---
"server": patch
---

Add `search_chats` and `read_chat` to Platform MCP so members with `chat:read` can search historical commands and tool inputs/results and inspect redacted message context across organization-captured sessions, including team, personal and unclassified provider accounts. Search progresses through bounded session and message windows with explicit coverage, narrowing filters and oldest/newest sort order. Reads support resumable redacted text chunks, enforce exact project access, and audit session reads.
