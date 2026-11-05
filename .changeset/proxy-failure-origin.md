---
"server": patch
"tunnel": patch
---

Add opt-in hooks that distinguish local request setup failures, private-connection capacity, and platform availability checks for MCP connection history. Nothing enables them yet, so connection history and request responses do not change. Tunnel gateways now label their own forward-token refusals with an internal status.
