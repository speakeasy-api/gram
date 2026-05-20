---
"server": patch
---

Anthropic prompt caching now actually takes effect for assistant chats. The `/chat/completions` proxy used to strip `cache_control` markers off the request body before forwarding to OpenRouter, so every Anthropic call billed at the full input rate. The proxy now preserves the markers at the top level, on tool definitions, and on message content blocks, so Claude requests with stable prefixes can serve from cache.
