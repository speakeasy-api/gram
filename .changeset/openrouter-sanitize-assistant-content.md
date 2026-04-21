---
"dashboard": patch
---

Fix chats breaking when switching providers mid-conversation. Assistant turns that contained both a text reply and a tool call could cause the next turn to fail with a validation error on some provider routes, leaving the conversation unrecoverable. Affected chats now continue to work seamlessly across providers.
