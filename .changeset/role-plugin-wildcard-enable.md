---
"server": patch
---

Enabling an MCP server no longer adds it to every plugin assigned to a role that can connect to all servers. A role's plugins now pick up a newly enabled server only when the role's grant names that server. The server still joins the Default plugin as before.
