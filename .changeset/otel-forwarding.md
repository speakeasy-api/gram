---
"server": minor
"dashboard": minor
---

Add OTEL forwarding: customers can configure a URL and headers on the Org Logs page, and a body-tee middleware mirrors every payload received on `/rpc/hooks.otel/v1/*` to that endpoint. Forwarding is org-wide, async (bounded worker pool, fire-and-forget on failure), capped at 4 MiB per request, and gated behind `org:admin` for writes / `org:read` for reads. Header values are encrypted at rest and never returned by the API.
