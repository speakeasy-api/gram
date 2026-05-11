---
"server": patch
---

OpenRouter responses indicating exhausted credits now surface as 402 Payment Required to chat callers instead of a generic 5xx, and the chat-resolution analyzer stops burning retries against a request that cannot succeed.
