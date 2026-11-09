---
"server": patch
---

Connection health now records when a direct server rejects the sign-in on `initialize`, `tools/list`, or any other request, not only on tool calls. Repeated rejections fold into a few rows.
