---
"server": minor
---

Platform MCP members now see their own activity without organization-wide permissions. The list_chats, search_chats and read_chat tools cover the caller's own chats and the chats of the agents they currently own, and the search_tool_calls and list_recent_tool_calls tools cover the tool calls of their own sessions and their agents. A chat:read or log:read grant on the project still widens each tool to the whole project, and no filter widens a member's view.
