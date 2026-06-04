---
"@gram-ai/elements": minor
---

Add hooks for backing the chat with a server-side assistant:

- `ElementsConfig.transport` accepts a `ChatTransport` or a factory `(ctx) => ChatTransport`. The factory is invoked inside the provider and receives `getChatId()` / `setChatId()`, so a consumer transport can read the active chat id at send time and adopt a backend-minted id without reaching into internals.
- `ElementsConfig.history` gains `threadListFilters` (extra query params forwarded to the thread list, e.g. to scope it to one backend conversation set) and `deferThreadIdMinting` (let the backend own chat-id creation instead of client-minting).
- `ElementsConfig.allowMessageEdit` (default `true`) hides the inline edit affordance on user messages — set to `false` when paired with a server-side transport that can't honour assistant-ui's local branch rewriting.
- Export `convertGramMessagesToUIMessages` and the `GramChat` types for building a custom transport against the chat service.
