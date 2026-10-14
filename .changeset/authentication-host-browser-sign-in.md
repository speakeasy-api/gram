---
"server": minor
---

People can now sign in through a shared authorization server on the authentication host. It serves registration, authorization and consent there, including federated login and connecting upstream services, and its token endpoint accepts the authorization code and refresh token grants. Its metadata and the protected resource metadata of its MCP servers point MCP clients to it, while the IdP and upstream login callbacks stay on the platform host.
