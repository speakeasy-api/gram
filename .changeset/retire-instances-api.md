---
"server": minor
---

Remove the `instances` API (`/rpc/instances.get` and `/rpc/instances.invoke/tool`). Only the legacy `@gram-ai/sdk` framework adapters called it, and it has received no traffic in the last 30 days. Connect to toolsets through their MCP server URL instead.
