---
"server": patch
---

Stop rejecting proxied MCP tool calls that do not echo back the internal `x-gram-toolset-id` property. Gram no longer adds that property to tool schemas served by the remote MCP proxy, so calls to remote and tunneled MCP servers succeed even when the model omits the value or invents its own.
