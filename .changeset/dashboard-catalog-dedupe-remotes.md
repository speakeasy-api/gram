---
"dashboard": patch
---

Fix the catalog Add Server dialog rendering duplicate-looking endpoint checkboxes when a registry entry exposes the same streamable-http URL twice (e.g. Datadog publishes an OAuth variant and an API-key variant under one URL). Same-URL remotes are now collapsed in the UI, matching the backend's deploy-time behavior of picking the first URL match.
