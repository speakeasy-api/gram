---
"server": patch
---

Gram Functions tool calls now size their Fly concurrency limits to real execution capacity (so memory bumps no longer inflate the request cap), return a retryable `429 + Retry-After` when a runner is saturated instead of dropping the connection, and retry tool-call POSTs only on safe pre-response transport errors.
