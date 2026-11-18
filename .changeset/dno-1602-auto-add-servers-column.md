---
"server": patch
---

Internal: adds the `plugins.auto_add_servers_enabled` column, which will let an admin choose whether a plugin's role audiences add and remove its MCP servers automatically. It defaults to `false`. Nothing reads it in this release. The follow-up wires it into role delivery and the dashboard.
