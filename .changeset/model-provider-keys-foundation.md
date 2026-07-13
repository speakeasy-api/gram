---
"server": minor
---

Projects can now store their own model provider API keys (BYOK), scoped per responsibility slot with a fallback chain: a slot-specific key wins over the project default key, which wins over the platform key. Keys are validated with the provider on save, stored encrypted, and never returned by the API. Configuration is gated behind the custom model keys product feature; with no keys configured, behavior is unchanged.
