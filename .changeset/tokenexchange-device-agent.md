---
"server": minor
---

Add a `tokenExchange` service for device-agent enrollment (DNO-383). `tokenExchange.exchange` trades an org-scoped `agent` API key plus a vouched user email for a long-lived, per-user API key carrying the `agent` and `hooks` scopes: the email is verified to belong to a real member of the authenticated org, the user's prior device-agent key is rotated (revoked), and the raw key is returned exactly once. `agent.getPlugins` now derives the enrolled user from the authenticated key owner and makes the `email` query parameter optional (backward-compatible: the plugin set is still resolved by organization).
