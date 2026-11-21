---
"server": patch
---

Daily connection outcomes now credit the owner's next connection with fixing a refused, already used sign-in. Days older than about 11 days at deploy keep the old count; newer days change on the next daily rebuild. Adds a read, not used yet, of who cannot connect to an MCP server right now.
