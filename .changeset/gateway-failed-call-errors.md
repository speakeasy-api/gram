---
"server": patch
---

A failed agent gateway tool call now records the error the agent was told on its Tool logs row. A describe_server or describe_tools that cannot reach a member is logged as a failure on the gateway's discovery row, and the member gets a failed row with the reason, listing only the tools the call asked of it.
