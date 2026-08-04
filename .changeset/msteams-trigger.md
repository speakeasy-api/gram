---
"server": minor
---

Add Microsoft Teams as an assistant trigger source. Bot Framework activities (messages, reactions, membership and installation updates) posted to a trigger webhook are verified against Microsoft's signing keys and dispatched to assistants with the same filtering (event type allowlist + CEL) as other webhook triggers.
