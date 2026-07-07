---
"@gram-ai/elements": minor
"dashboard": patch
---

Hide the chat composer for project assistant threads the signed-in caller didn't create. Elements gains an opt-in `history.isOwnChat` callback (mirrors `resolveCreator`) that reports whether the caller owns a thread-list chat; the dashboard wires it up so admins who open another member's chat via their `chat:read` grant see a read-only transcript instead of a "chat not found" error on send — the backend has always rejected replies into a chat you don't own, this just stops the UI offering an action that was never going to succeed.
