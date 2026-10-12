---
"server": patch
---

Identity chaining client registration always requests the JWT-bearer grant. When the provider's metadata advertises `authorization_code` (and a `code` response type), it also sends `redirect_uris` (the remote login callback), `response_types: ["code"]`, `authorization_code` and, if advertised, `refresh_token`; providers that support only JWT bearer get a JWT-bearer-only registration as before. The callback origin is recorded on the new client like interactive registrations. Providers that require redirect URIs accept the registration, and the client can complete the one-time owner approval some providers require through a normal sign-in. When a provider's registration response omits its grants, the binding stays "unknown grants" and the remediation now tells the administrator to check that the client allows JWT-bearer (and any owner approval) before confirming its grants.

Logs now include the provider's `error_description` for failed identity chaining exchanges and redemptions, and the `error` and `error_description` of rejected registrations, sanitized and capped. This text is never returned in API responses or written to audit rows.

Token endpoint calls for identity chaining and client credentials, and identity chaining registration, send a `Speakeasy-Gram/1.0` User-Agent. The remote MCP proxy still forwards the caller's User-Agent and sends `Speakeasy-Gram/1.0` only when the caller sent none.
