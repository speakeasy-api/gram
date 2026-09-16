---
"server": patch
---

Add an injectable Okta Management API client (`server/internal/thirdparty/okta`) with private_key_jwt + DPoP authentication, nonce and rate-limit handling, and an in-memory fake for tests. The client is not wired into `deps.go` yet; wiring lands with the first consumer.
