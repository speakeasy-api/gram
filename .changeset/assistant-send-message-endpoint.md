---
"server": patch
---

Add an endpoint for a dashboard user to send a message to an assistant. The reply is delivered asynchronously — the response returns the chat to poll for it. The caller chooses the conversation thread via a correlation key (send the user id for one continuing thread per user, or a fresh value to start over), and can pass an idempotency key so a retried send doesn't enqueue the message twice.
