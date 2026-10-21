---
"server": patch
---

Hosted MCP plugin entries, install page metadata, and assistant attachments are now read only under the toolset's canonical MCP server. Rows still stored under the retired toolset key are no longer read, except that a plugin entry removed under the toolset key still keeps role delivery from re-adding the server. A plugin entry left under the toolset key is listed with backend `unresolved` instead of being dropped.
