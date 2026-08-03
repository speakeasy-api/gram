---
"server": minor
---

Enforce allow-all shadow MCP policies in the hook path. Under an allow_all policy every non-Gram-hosted MCP server is permitted unless a risk_policy:block grant names its URL; bypass grants and the fail-closed inventory checks remain block_all concepts and are skipped. Projects are now limited to one enabled shadow MCP blocking policy so dispositions can never conflict at enforcement time.
