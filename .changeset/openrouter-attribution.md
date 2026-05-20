---
"server": patch
---

Outbound OpenRouter chat completions now carry a session ID, user, source metadata, and distributed-trace identifiers so OpenRouter's dashboard can group requests per conversation and roll up cost per customer, and so Datadog traces correlate with OpenRouter's request records.
