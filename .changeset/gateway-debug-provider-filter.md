---
"server": minor
"dashboard": minor
---

Gateway Debug can filter and count connections by upstream provider. A session's providers are the provider sign-ins its person or agent is admitted to on that sign-in server; "Speakeasy only" selects sessions with none. The session list API, its facets, `platform_list_user_sessions` and `list_gateway_connections` accept the same `providers` and `exclude_providers` filters, and `list_gateway_connections` returns each connection's provider ids. The connection list's client grouping is labelled Client.
