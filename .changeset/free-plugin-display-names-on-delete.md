---
"server": patch
---

Deleting an MCP server or toolset now also removes it from any plugins it was attached to, so a same-named replacement can attach to the Default plugin instead of failing with "attach mcp server to default plugin". Display name collisions between different servers in a plugin are de-conflicted with a numeric suffix instead of failing the endpoint create or server enable that triggered the attach.
