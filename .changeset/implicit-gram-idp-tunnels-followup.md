---
"server": minor
"dashboard": patch
---

Implicitly gate private remote/tunneled MCP servers with a project-default Gram issuer when no explicit `user_session_issuer` is configured: the OAuth surface (well-known metadata, DCR, authorize/connect/token/revoke, first-party connect) now works out of the box, identity auth (API keys, chat sessions) keeps working, and unauthenticated requests get a `WWW-Authenticate` challenge advertising the resource metadata. Also fix `remoteSessionClients.delete` silently no-oping for organization-level clients, and in the dashboard: derive the tunnel gateway URL from the environment instead of a hardcoded prod hostname, detach (rather than delete) remote identity providers from a server, and mint user-session tokens for implicitly gated servers so the tools list works without auth configuration.
