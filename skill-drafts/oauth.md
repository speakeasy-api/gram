---
name: oauth
description: Lookup index for authoritative OAuth, AS metadata, DCR, PKCE, OAuth 2.1, OIDC, and JWT/JWS spec URLs. Activate when a task involves OAuth flows, AS metadata, DCR, PKCE, OAuth 2.1 specifics, or OIDC schema/JWT-shape questions and the agent needs a spec reference. NOT for "how does Gram do OAuth today" (that's gram-legacy-oauth) or "OAuth in MCP" (that's mcp-spec).
---

# OAuth Spec Reference Index

This skill is a lookup index. It points at authoritative spec URLs; it does not restate the specs. Assume the reader already knows OAuth.

## Core OAuth 2.0 / 2.1

- **RFC 6749** — The OAuth 2.0 Authorization Framework. Defines the four grant types, the authorization endpoint, and the token endpoint.
  https://datatracker.ietf.org/doc/html/rfc6749
- **RFC 6750** — Bearer Token Usage. How to send an access token in the `Authorization: Bearer` header (and `WWW-Authenticate` error responses).
  https://datatracker.ietf.org/doc/html/rfc6750
- **OAuth 2.1 (draft-ietf-oauth-v2-1)** — Consolidates RFC 6749 + RFC 6750 + PKCE + security BCP, removes implicit and password grants, mandates PKCE for all clients.
  https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1

## Discovery & Registration

- **RFC 8414** — OAuth 2.0 Authorization Server Metadata. The `/.well-known/oauth-authorization-server` document and its fields (`issuer`, `authorization_endpoint`, `token_endpoint`, `jwks_uri`, `registration_endpoint`, etc.).
  https://datatracker.ietf.org/doc/html/rfc8414
- **RFC 7591** — Dynamic Client Registration. The `/register` endpoint and client metadata schema.
  https://datatracker.ietf.org/doc/html/rfc7591

## PKCE & Security

- **RFC 7636** — Proof Key for Code Exchange. `code_verifier` / `code_challenge` / `code_challenge_method` (S256).
  https://datatracker.ietf.org/doc/html/rfc7636
- **RFC 7009** — Token Revocation. The `/revoke` endpoint contract.
  https://datatracker.ietf.org/doc/html/rfc7009

## OIDC & JWTs

- **OpenID Connect Core 1.0** — Identity layer on top of OAuth 2.0. Most relevant for JWT claim semantics: `iss`, `sub`, `aud`, `exp`, `iat`, `nonce`, `azp`. See section 2 (ID Token) and section 5 (UserInfo).
  https://openid.net/specs/openid-connect-core-1_0.html
- **RFC 7519** — JSON Web Token (JWT). Compact claim format and the registered claim names (`iss`, `sub`, `aud`, `exp`, `nbf`, `iat`, `jti`).
  https://datatracker.ietf.org/doc/html/rfc7519
- **RFC 7515** — JSON Web Signature (JWS). Signing structure, header parameters (`alg`, `kid`, `typ`), and serialization.
  https://datatracker.ietf.org/doc/html/rfc7515

## Implementation crib notes (Gram-specific divergences)

- Gram signs session JWTs with **HS256** (symmetric), not the asymmetric `RS256`/`ES256` that OIDC mandates for ID tokens. Deliberate divergence: Gram's tokens are first-party session credentials, not federated identity assertions. No `jwks_uri` is published for them.
- Gram does not use the **Device Authorization Grant (RFC 8628)**. If a feature seems to call for it, push back before implementing.
- For PKCE, **always require S256** (`plain` is forbidden by OAuth 2.1 and the security BCP).
- AS Metadata (RFC 8414) and DCR (RFC 7591) are the load-bearing specs for the "Remote OAuth Clients for Private Repos" RFC — start there when designing endpoint shapes.
- For project-specific OAuth shape (current Gram flows, schemas, handlers), see `prompt.md` and the `gram-legacy-oauth` skill rather than re-reading these RFCs.
