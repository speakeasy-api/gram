---
"server": patch
"dashboard": patch
---

Linking, changing or removing an MCP server's environment now requires project-wide `environment:read` in addition to `mcp:write`, the same authority the environments service already requires to link an environment to a source or toolset. The same check applies when a linked server is repointed at another backend, when a remote MCP source with a linked server changes URL, and when the key of a tunnel with a linked server is rotated. Environment link and unlink changes are now recorded as their own audit events. The MCP server settings page disables Remote URL edits and tunnel key rotation, with the reason, for callers who lack that authority, and the audit log names the new events.
