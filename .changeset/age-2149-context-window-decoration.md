---
"server": minor
---

Decorate `/chat/completions` responses with the upstream model's context window via a `gram_metadata` extension. The size is fetched from OpenRouter's per-model endpoints listing (smallest `context_length` across providers) and cached for 72h. The streaming path injects the value into the final SSE frame.
