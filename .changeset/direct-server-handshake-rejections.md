---
"server": patch
---

Connection health now records when a direct server rejects the sign-in on `initialize`, `tools/list`, or any other request, not only on tool calls. The row's sub-reason names the request, such as `initialize` or `tools_list`. Repeated rejections fold into a few rows.
