---
"dashboard": patch
---

Auto-create a default MCP endpoint when importing a remote MCP server as a source. Previously the import created the server but required the user to add an endpoint by hand before it could serve traffic. The import flow and the detail-page re-link flow now pre-stage a default platform endpoint with slug `{orgSlug}-{random}`. Endpoint creation is best-effort: a failure leaves the source intact and surfaces a warning toast, and the server stays disabled so the endpoint begins serving once the user enables it.
