---
"server": patch
---

Anthropic inference hooks now return a verdict within the provider's default five second timeout instead of being cut off mid-evaluation. Feature flags are resolved once per delivery rather than once per scanned input, inputs are scanned concurrently, and when a long transcript still cannot be fully evaluated in time the request is denied with a retry message while the messages that were evaluated are checkpointed, so the retry only scans the remainder.
