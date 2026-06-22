---
"server": patch
---

Deleting a chat that backs an active assistant is now blocked and returns a conflict. Previously the chat could be soft-deleted out from under a running assistant, which broke the assistant's ability to load its conversation and could leave it silently wedged.
