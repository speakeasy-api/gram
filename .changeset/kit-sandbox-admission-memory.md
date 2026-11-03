---
"server": patch
---

Kit assistants on managed GKE start again: their sandbox containers now request enough memory to pass the cluster's sandbox admission policy, and a Kit resource budget that would be rejected now fails at startup instead of at first use.
