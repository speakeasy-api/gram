---
"server": patch
---

Claude chats imported through the Anthropic Compliance API now record the owner's email as the session's user label, falling back to the Anthropic user id when the feed has no email. Sessions owned by employees who are not members of the Speakeasy organization previously showed only an opaque Anthropic id, so searching Agent Sessions by email found nothing. Existing sessions pick up the email when they next receive a message.
