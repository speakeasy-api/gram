---
"server": patch
---

A failed hosted tool call, direct or through a gateway, now records its most specific client-safe error on its Tool logs row. A describe_server that cannot reach its member is logged as a failure on the gateway's discovery row. For describe_server and describe_tools, each member that could not be reached gets a failed row with the reason, listing only the tools the call asked of it.
