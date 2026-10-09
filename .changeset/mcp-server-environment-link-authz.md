---
"server": patch
"dashboard": patch
---

Linking, changing or removing an MCP server's environment now requires `environment:read` across the project and on every environment the change affects, in addition to `mcp:write`, so an exclusion on one environment refuses it. The same check applies when a linked server is repointed at another backend, when a remote MCP source with a linked server changes URL, and when the key of a tunnel with a linked server is rotated. The source and toolset environment link endpoints now apply the per-environment check too (including to the link being replaced or removed) and refuse a toolset from another project. Environment link and unlink changes on MCP servers, sources and toolsets are recorded as their own audit events. The MCP server settings page disables Remote URL edits and tunnel key rotation, with the reason, when the server reports the caller lacks that authority, and the audit log names the new events.
