---
"server": patch
---

Identity map syncs that lose the single-writer lock now defer to the running replacement instead of failing and retrying.
