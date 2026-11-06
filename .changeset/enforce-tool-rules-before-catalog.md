---
"server": patch
---

Rules that block MCP tools by type, like "block destructive tools", now apply to tools Speakeasy hasn't seen before on remote MCP servers. Speakeasy records a server's tools whenever they're listed, and looks up any unrecorded tool before running it, so these rules no longer depend on someone opening the server in the dashboard. A call is refused if the tool can't be checked.
