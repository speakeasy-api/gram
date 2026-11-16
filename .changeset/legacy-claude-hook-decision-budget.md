---
"server": patch
---

Answer legacy Claude Code hook requests (`/rpc/hooks.claude`) within 5 seconds. When a policy check takes longer, the endpoint now responds from the organization's hooks fail-open setting instead of letting older curl-based hook scripts time out and block with "HTTP 000". Fail-open organizations get a pass-through response; fail-closed organizations get a block that says the security check did not finish in time. The event is still recorded, and the slow check finishes in the background.
