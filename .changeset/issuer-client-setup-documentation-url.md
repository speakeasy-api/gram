---
"server": minor
"dashboard": patch
---

feat: surface issuer setup documentation when creating clients. `remote_session_issuer` records now expose a `client_setup_documentation_url`, settable on create and update across the project-scoped, org-admin, and platform-admin (global) issuer surfaces. The dashboard edits it on the issuer Settings tab and shows it on the Overview tab alongside the discovered RFC 8414 `service_documentation`. Both are linked from the New Client sheet — as **Client Setup Documentation** and **Service Documentation** — so customers can set up an OAuth client with the provider themselves, owning its credentials, access, and rate limits rather than sharing a Gram-owned client. `client_setup_documentation_url` must be an absolute `http(s)` URL (validated with `urls.IsAbsoluteHTTP`, since it is rendered as a link); an empty string clears it.
