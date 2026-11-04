---
"server": patch
---

An MCP connection no longer breaks when a client disconnects while Speakeasy is refreshing its provider token. The refresh now finishes and saves the new token, so the person does not have to reconnect.
