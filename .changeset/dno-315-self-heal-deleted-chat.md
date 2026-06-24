---
"server": patch
---

A chat that backs an active assistant now clears its soft-deleted state automatically when it receives another message, so an assistant whose chat was deleted out from under it recovers instead of staying wedged. Chats with no active assistant are left deleted, so this never resurrects a chat a user intentionally deleted.
