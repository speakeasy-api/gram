---
"server": patch
---

fix(remotemcp): harmonize the forbidden JSON-RPC code across the hosted `/mcp` and proxied `/x/mcp` surfaces so a usage-limit denial reports the same code on both. The hosted `MCPError` can now carry a structured `data` payload, matching the proxy's `RejectError.Data`.
