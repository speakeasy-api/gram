---
"dashboard": patch
---

Fix "Server not found" on the catalog detail page for servers whose `registrySpecifier` was not in the first page of the unfiltered catalog list (e.g. `com.pulsemcp.mirror/datadog`). The backend `ListCatalog` aggregates across registries and caps results at 100, and only emits a `nextCursor` for single-registry queries, so the detail page could never reach deep entries. The lookup now passes the specifier's last path segment as a `search` filter so the upstream registry narrows the result set and the target server lands in the first page.
