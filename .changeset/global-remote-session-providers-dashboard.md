---
"dashboard": minor
---

Add the Global Remote Session Providers admin UI to the platform-admin Developer Toolkit. A new "Global" tab exposes a modal for curating platform-wide remote session providers (issuer + client pairs with no owning project or organization), backed by the `adminRemoteSessions` API: list/search providers, create (1:1 issuer + client), edit issuer and client fields (client secret write-only, client ID immutable after creation), view any additional clients read-only, and delete. Only platform admins (Speakeasy employees) can reach the toolkit, so the surface is staff-only.
