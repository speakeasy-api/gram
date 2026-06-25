---
"server": patch
"dashboard": patch
---

Pinned chats: pin/unpin conversations on the /chat page. Pinned chats surface in a dedicated "Pinned" section above Recent Chats. Adds a `setPinned` chat API and a `pinned` filter on `listChats`, backed by the `chats.pinned_at` column.
