---
"server": patch
---

Connection health now records when a direct server rejects the sign-in on `initialize`, `tools/list`, or any other request, not only on tool calls. The row's sub-reason names the request, such as `initialize` or `tools_list`, and repeats fold per request. A rejection counts only when the request that reached the server carried the credential, so a redirect that dropped it or a skipped header is no longer blamed on the credential. A failed renewal of a server's own credential during a provider outage is recorded as that outage.
