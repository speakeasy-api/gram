---
"server": patch
---

The Platform MCP `get_xaa_readiness` tool now reports an MCP server's identity chaining bindings (state, remediation, the configured scopes, the scopes actually requested after dropping OpenID Connect scopes, and whether chaining serves it, judged the way the runtime selects a direct or tunneled upstream) and the federated sign-in callback URL to register in the identity provider, including for clients with a recorded callback origin when no outbound origin is pinned. Every live binding for the upstream is listed. Preparing identity chaining checks organization administration before any other grant validation when it would change an organization-owned client's grants.
