---
"server": patch
---

Classify AI integration poll failures caused by provider outages (persistent 429/5xx or transport errors) so they skip the in-run Temporal retries and defer to the schedule-level failure backoff, and surface the provider's final HTTP status and error body when the guardian retry client exhausts its budget instead of the opaque "giving up" message.
