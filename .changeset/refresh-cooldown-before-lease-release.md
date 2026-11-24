---
"server": patch
---

When a provider's token endpoint is down, a request that was waiting on another request's token refresh now waits out the outage instead of sending the same refresh token again. Before, the refresh lock was released a moment before the outage was recorded, so a request arriving in that gap could make a second call to the provider.
