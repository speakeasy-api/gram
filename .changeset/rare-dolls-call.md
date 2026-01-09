---
"server": minor
---

Added two new API endpoints for uploading and serving chat attachments.

The `/rpc/assets.serveChatAttachment` endpoint can be accessed with an API key or session cookie. `Gram-Project` is not used on that endpoint to make it easy for session-based clients to embed attachments in chat such as with `<img>` tags for images e.g. `<img src="/rpc/assets.serveChatAttachment?id=...&project_id=..." />`.
