---
"server": patch
"dashboard": patch
---

Fixed `GET /rpc/chat.creditUsage` authentication so org-scoped credit usage works correctly for customers with multiple projects, requiring only session auth and no longer allowing chat-session access.
