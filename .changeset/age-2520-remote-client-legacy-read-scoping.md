---
"server": patch
---

Remote OAuth client lookups no longer surface clients whose bound user session issuer lives in a different project or has been soft-deleted. The legacy `user_session_issuer_id` fallback path now scopes both the client and its user session issuer to the request's project and excludes soft-deleted clients, remote issuers, and user session issuers — matching the join-table read path. In practice this is a no-op for existing data (no production rows are in that state); it closes the gap going forward.
