---
"server": minor
---

Public well-known OAuth/MCP metadata responses now send `Cache-Control: public, max-age=60` and a strong `ETag` with `If-None-Match` 304 revalidation, so clients and proxies can cache them. The OAuth Client ID Metadata Document keeps `max-age=3600` and gains an `ETag`. This is a prerequisite for fronting these responses with an ingress cache or CDN.
