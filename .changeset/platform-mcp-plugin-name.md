---
"server": patch
---

Rename the first-party Platform MCP package to `speakeasy` and its MCP server to `platform`, so agents label its tools `plugin:speakeasy:platform` instead of repeating `platform-mcp` twice. Claude Code installs become `/plugin install speakeasy@speakeasy`; the Cursor and Codex packages keep a client suffix (`speakeasy-cursor`, `speakeasy-codex`) because every package root shares one repository.
