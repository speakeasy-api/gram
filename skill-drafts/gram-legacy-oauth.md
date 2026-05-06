---
name: gram-legacy-oauth
description: Anatomy of Gram's current production OAuth surface — the Postgres tables, Redis caches, and tool-proxy code paths under `server/internal/oauth/` and `server/internal/auth/chatsessions/` — together with the migration principles guiding the rewrite to split client sessions from remote sessions. Activate when a task touches the OAuth proxy, external OAuth provider integration, chat-session JWTs, or anything wired to `toolsets.oauth_proxy_server_id` / `toolsets.external_oauth_server_id`; especially when deciding whether a piece of legacy structure is being deliberately preserved or deliberately removed by the in-flight RFC.
metadata:
  relevant_files:
    - "server/internal/oauth/**/*.go"
    - "server/internal/auth/**/*.go"
    - "server/internal/auth/chatsessions/**/*.go"
    - "server/internal/auth/sessions/**/*.go"
    - "server/internal/gateway/proxy.go"
    - "server/internal/mcp/impl.go"
    - "server/internal/mcp/rpc_tools_call.go"
    - "server/internal/toolconfig/toolconfig.go"
    - "server/database/schema.sql"
    - "prompt.md"
    - "oauth-schema.sql"
    - "redis-oauth-schema.go"
    - "jwt-schema.go"
---

Gram's legacy OAuth surface is one component playing two distinct OAuth roles. Inside the same `server/internal/oauth/` package, Gram is simultaneously **(a)** an OAuth 2.1 Authorization Server to MCP clients (DCR, `/authorize`, `/token`, grant + token storage) and **(b)** an OAuth client to remote upstream providers (custom and Gram-managed flows, plus the playground's external-MCP integration). Those two responsibilities share data structures, share a directory, share encryption helpers, and tunnel upstream credentials through the AS's own access-token cache as `ExternalSecrets`. The RFC ("Remote OAuth Clients for Private Repos", `prompt.md`) is fundamentally about un-mixing them.

This skill describes what's in production today, names the moving pieces, and summarises the principles the rewrite is converging on. Use it to recognise legacy structure on sight and to judge whether a structure is being preserved deliberately or removed deliberately.

## What "legacy OAuth" means

Gram's MCP servers can be configured with one of two mutually-exclusive OAuth modes (enforced at schema level by `toolsets_oauth_exclusivity` on `toolsets`):

- **OAuth proxy mode** (`toolsets.oauth_proxy_server_id`). Gram is the AS. MCP clients DCR with Gram, hit Gram's `/authorize` and `/token`, and Gram in turn does an upstream OAuth dance against a configured `oauth_proxy_provider`. The upstream tokens are stored alongside the Gram-issued access token as `ExternalSecrets` on the cached `Token`, and later lifted onto outbound HTTP requests at tool-call time.
- **External OAuth server mode** (`toolsets.external_oauth_server_id`). The MCP client speaks OAuth directly to an external Authorization Server whose RFC 8414 metadata Gram exposes verbatim from `external_oauth_server_metadata.metadata`. Gram is not the AS in this mode and does not exchange codes itself; it simply validates whatever bearer the client presents.

Independent of those two toolset-level modes, the **playground** has its own OAuth client used only inside the dashboard (`user_oauth_tokens` + `external_oauth_client_registrations`). It is **out of scope** for the rewrite; do not touch it.

The architectural smell the RFC removes: the Postgres + Redis schema makes no clean distinction between _the session Gram has with its MCP client_ and _the session Gram has with the upstream provider_. Both live on the `Token` Redis document, share its single TTL, and are keyed on the access-token value rather than on a stable session id.

## Postgres surface

| Table                                 | What it stores                                                                                                                                                                                                                                                                                                                                                                                 | Who writes / reads it                                                                                                                 | Key                                                     | Status                                                                                                                                                                                                                                                              |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `external_oauth_server_metadata`      | Verbatim RFC 8414 AS metadata for "external OAuth server" mode toolsets.                                                                                                                                                                                                                                                                                                                       | Written by management API (toolset config); read by the MCP handler / `/.well-known` proxy.                                           | `(project_id, slug)` unique                             | Migrates to `remote_oauth_issuer` (RFC §5).                                                                                                                                                                                                                         |
| `oauth_proxy_servers`                 | Container record per OAuth-proxy toolset. Just an id + slug + optional `audience` claim that the Gram-issued AS token will carry.                                                                                                                                                                                                                                                              | Written by management API; read by `/oauth/{mcpSlug}/*` handlers in `server/internal/oauth/impl.go`.                                  | `(project_id, slug)` unique                             | Replaced by `client_session_issuer` (RFC §5).                                                                                                                                                                                                                       |
| `oauth_proxy_providers`               | The upstream provider config attached to a proxy server: `provider_type` (`custom` \| `gram`), the three endpoints (`authorization_endpoint`, `token_endpoint`, `registration_endpoint`), advertised capabilities (`scopes_supported`, `grant_types_supported`, etc.), `security_key_names`, and a JSONB `secrets` blob (typically `client_id`, `client_secret`, optional `environment_slug`). | Written by management API; read by every `/oauth/*` handler and by `mcp/impl.go` to decide between `gram`/`custom` provider plumbing. | `(project_id, slug)` unique                             | **Multiple deprecations.** `secrets` JSONB and `security_key_names` array are explicitly slated for removal (`prompt.md` goal #3); `provider_type='custom'` will not survive in its current form. The whole table maps onto `remote_oauth_client` in the new world. |
| `oauth_proxy_client_info`             | Persisted DCR registrations from MCP clients that registered with Gram-as-AS (`/oauth/{mcpSlug}/register`). Holds `client_id`, `client_secret`, expiry, `redirect_uris`, etc.                                                                                                                                                                                                                  | Written by `ClientRegistrationService` on DCR; read on every `/authorize` and `/token` call.                                          | `client_id` (PK) — note the absence of project scoping. | Stays conceptually but moves under whatever AS-side concept replaces `oauth_proxy_servers`.                                                                                                                                                                         |
| `external_oauth_client_registrations` | Org-level credentials Gram obtained when **Gram itself** DCR-registered as a client of an external MCP server (Playground feature). Holds `client_id`, encrypted `client_secret`, expiry.                                                                                                                                                                                                      | Written by playground OAuth flow in `external_oauth.go`; read on subsequent token exchanges.                                          | `(organization_id, oauth_server_issuer)` unique         | **Out of scope** — playground only.                                                                                                                                                                                                                                 |
| `user_oauth_tokens`                   | Per-user, per-toolset access/refresh tokens obtained via the playground's external-MCP OAuth client flow. Tokens encrypted at rest by `encryption.Client`.                                                                                                                                                                                                                                     | Written/read by `ExternalOAuthService` in `server/internal/oauth/external_oauth.go`.                                                  | `(user_id, organization_id, toolset_id)` unique         | **Out of scope** — playground only.                                                                                                                                                                                                                                 |

FK touch points from `toolsets`: `external_oauth_server_id` → `external_oauth_server_metadata(id)` and `oauth_proxy_server_id` → `oauth_proxy_servers(id)`. The CHECK constraint `toolsets_oauth_exclusivity` enforces "at most one mode set". `mcp_servers` carries the same two FKs.

## Redis surface

All five OAuth-domain Redis types live behind `cache.TypedCacheObject[T]` and are JSON-serialised. See `server/internal/oauth/storage.go` and `server/internal/oauth/external_oauth.go` for the in-tree definitions; `redis-oauth-schema.go` at the worktree root is the distilled summary.

| Type                   | Source file               | Cache key                              | Additional keys                                                        | TTL                                       |
| ---------------------- | ------------------------- | -------------------------------------- | ---------------------------------------------------------------------- | ----------------------------------------- |
| `Grant`                | `oauth/storage.go`        | `oauthGrant:{toolsetID}:{code}`        | none                                                                   | `time.Until(ExpiresAt)` (10 min standard) |
| `Token`                | `oauth/storage.go`        | `oauthToken:{toolsetID}:{accessToken}` | `oauthRefreshToken:{toolsetID}:{refreshToken}` if a refresh is present | `time.Until(ExpiresAt) + 24h`             |
| `OauthProxyClientInfo` | `oauth/storage.go`        | `oauthClientInfo:{mcpURL}:{clientID}`  | none                                                                   | `time.Until(ClientSecretExpiresAt) * 2`   |
| `UpstreamPKCEVerifier` | `oauth/storage.go`        | `upstreamPKCE:{nonce}`                 | none                                                                   | 10 min fixed                              |
| `ExternalOAuthState`   | `oauth/external_oauth.go` | `externalOAuthState:{stateID}`         | none                                                                   | `time.Until(ExpiresAt)` (~10 min)         |

Two structural points worth flagging:

1. **`Grant` and `Token` carry `ExternalSecrets []ExternalSecret`** — the upstream provider's access token, refresh token, optional expiry, and `SecurityKeys []string` denoting which OpenAPI security scheme(s) the bearer satisfies. The values live at `json:"-"` and are encrypted by `encryption.Client` before storage (see `grant_manager.go` ~L260–310 and `impl.go` ~L820–830). This is the channel through which an upstream credential rides on a Gram AS token.
2. **`Token` couples access + refresh into one document with one TTL** — the access-token expiry plus a 24h grace so the refresh entry outlives the access entry. The RFC explicitly splits these: a client-session access token becomes a JWT with no Redis row at all, and refresh tokens move to dedicated `(session_id, client_session_issuer_id)`-keyed Redis docs with independent TTLs.

`OauthProxyClientInfo` is also persisted to Postgres (`oauth_proxy_client_info`); the Redis copy is a hot-path cache.

## Chat-session JWTs (the manager the new tokens will reuse)

`server/internal/auth/chatsessions/` owns the only JWT manager Gram currently runs.

- **Signing.** HS256 with `GRAM_JWT_SIGNING_KEY` (env). No JWKS, no public-key OIDC machinery.
- **Claims.** `ChatSessionClaims` extends `jwt.RegisteredClaims` with Gram-shaped fields: `org_id`, `project_id`, `organization_slug`, `project_slug`, optional `external_user_id`, `api_key_id`. Standard claims used: `ID` (JTI, UUIDv4 used for revocation), `Audience` (set to the embed origin), `IssuedAt`, `ExpiresAt`. `Issuer` and `Subject` are deliberately empty strings today — the rewrite fills `sub`.
- **Header.** Tokens reach clients via `Gram-Chat-Session`.
- **Revocation.** `RevokedToken{ JTI, RevokedAt }` is cached at `chat_session_revoked:{jti}` with a 24h TTL; `Manager.ValidateToken` rejects any token whose JTI is in the cache.
- **Authorize.** `Manager.Authorize` validates the JWT and stamps `contextvalues.AuthContext` with the org/project/api-key fields. **It explicitly leaves `SessionID` nil** — the comment notes that a populated `SessionID` implies dashboard-authenticated, and chat sessions are not that.

The new client-session access token will be issued by this same manager: same key, same HS256, same revocation cache. What changes is the _claim shape_ (OIDC-shaped, populated `sub` and `aud`) and the _audience semantics_ (`aud = toolset slug`).

NOTE: `server/internal/auth/chatsessions/jwt.go` currently has uncommitted in-progress edits (a stray `https://` token at line 26 and several lines of garbled prose / draft Go below the `ValidateToken` definition). Treat the live file as broken; the canonical pre-edit contents are reflected in `jwt-schema.go` at the worktree root.

## Token exchange in the tool proxy (`doHTTP`)

Today, lifting an upstream credential onto an outbound HTTP tool call happens inside the tool-proxy hot path, not in a dedicated layer. The chain:

1. **MCP handler collects bearers.** `server/internal/mcp/impl.go` (`servePublicHandler` / authentication branches around L450–500) inspects the toolset config. For an OAuth-proxy custom toolset, it calls `s.oauthService.ValidateAccessToken`, then iterates `oauthToken.ExternalSecrets` and pushes one `oauthTokenInputs{securityKeys, Token}` per secret onto the request payload. For an external-OAuth toolset it just passes the raw bearer through with empty `securityKeys`.
2. **Tools-list and tools-call read the same slice.** `mcp/rpc_tools_list.go` and `mcp/rpc_tools_call.go` walk `payload.oauthTokenInputs` to decide which tools require auth and to build the `userConfig` env for each call. In `rpc_tools_call.go` L403–425, for each HTTP-tool security entry whose type is `authorization_code` or `openIdConnect`, the matching token is written into the `*_ACCESS_TOKEN` env variables; for function tools with `auth_input.type == "oauth2"`, the token is written into `plan.Function.AuthInput.Variable`.
3. **`doHTTP` consumes the env, not the secret.** `server/internal/gateway/proxy.go` `doHTTP` does not see `ExternalSecret` directly — it only sees `env.UserConfig` / `env.SystemEnv` / `env.OAuthToken`. The `OAuthToken` field is populated only from the _first_ tokenInput whose `securityKeys` is empty (`rpc_tools_call.go` L193–200) and is consumed by the **external-MCP** path (`runExternalMCPCall`, `proxy.go` L780–790) where it's threaded through `externalmcp.BuildHeaders`. For OpenAPI-derived HTTP tools, the bearer reaches the wire only because `userConfig` already has the right `*_ACCESS_TOKEN` env var injected upstream.

This is the code path RFC goal #7 wants gone. The replacement is a layer that materialises remote credentials _before_ tool execution, decoupled from `doHTTP` and from the AS-side `Token` document.

## Migration principles

Summarised from `prompt.md`:

1. **Split client sessions from remote sessions in the data model.** A _client session_ is what Gram has with the MCP client (signed access JWT + Redis-backed refresh). A _remote session_ is what Gram has with each upstream provider on behalf of that client session, stored as its own Redis doc keyed `(session_id, client_session_issuer_id)` with independent TTLs for access and refresh.
2. **Decouple `oauth_providers` into `remote_oauth_issuer` + `remote_oauth_client`.** Issuer rows model auto-discovered upstream metadata (and gain a required `oidc bool`); client rows model the credentials Gram uses against an issuer. Schema is 1:N issuer→client; initial scope keeps it 1:1.
3. **Client-session access tokens become OIDC-shaped JWTs**, signed by the existing chat-session HS256 key. Required claims: `iss`, `sub`, `aud`, `iat`, `exp`. `aud` is the toolset slug — this lets validators reject chat-session JWTs presented as client-session tokens and vice versa. Valid `sub` URN shapes are `user:<id>`, `apikey:<uuid>`, and `anonymous:<mcp-session-id>`. **`role:<slug>` is not valid in `sub`** — roles are not authentication subjects. `urn.APIKey` stays a parallel URN kind (it is not a `PrincipalType`); both can occupy `sub`. Anonymous principals are MCP-only.
4. **Refresh tokens move to Redis docs keyed on `(session_id, client_session_issuer_id)`** rather than on the token value, with separate TTLs for access and refresh.
5. **Consent fix on `/authorize`.** Today the consent flow is incomplete and `consent_template.html` is unreferenced. The RFC requires the authorize endpoint to skip consent **only** when the user has previously consented for this `client_session_issuer` to access **all** of its `remote_session_tokens`. Existence of a session alone is not sufficient.
6. **No directory just called `oauth/`.** Each concern moves to a qualified directory — `client-session/`, `remote-session/`, etc. The package structure should refuse to mix the two roles.
7. **Lift token exchange out of `doHTTP`.** Remote credentials are materialised in a layer wholly decoupled from tool execution, and `doHTTP` consumes a homogenised result.
8. **`client_session_issuer` never generates its own session ids** — they are injected by the caller. The MCP handler injects `mcp_session_id`; the package itself stays agnostic about the source.

Out of scope for this work, even when you trip over it: `user_oauth_tokens`, `external_oauth_client_registrations`, and the Playground OAuth UX. Leave them alone.

## Where to look

- **OAuth-as-AS implementation.** `server/internal/oauth/impl.go` (route attachments at L155–179, `handleAuthorize` at L240+, consent-related branches around L420–425), `server/internal/oauth/grant_manager.go`, `server/internal/oauth/token_service.go`, `server/internal/oauth/client_registration.go`, `server/internal/oauth/pkce.go`. Provider plumbing under `server/internal/oauth/providers/` (`gram.go`, `custom.go`).
- **OAuth-as-client (playground).** `server/internal/oauth/external_oauth.go` (~1.1 KLOC; `ExternalOAuthState`, the `/oauth-external/*` handlers, success/failure page rendering).
- **Chat-session JWT.** `server/internal/auth/chatsessions/jwt.go`, `server/internal/auth/chatsessions/manager.go`. JWT secret comes in via `NewManager(... jwtSecret string)` and is fed `GRAM_JWT_SIGNING_KEY` at startup.
- **Auth context + RBAC seam.** `server/internal/auth/sessions/sessions.go`, `server/internal/contextvalues/`, and the RBAC `urn.Principal` definition at `server/internal/urn/principal.go` (where the RFC will add `PrincipalTypeAnonymous`).
- **MCP handler integration.** `server/internal/mcp/impl.go` (`oauthTokenInputs` type at L107, collection logic around L420–530, `authenticateToken` at L917+).
- **Tool proxy.** `server/internal/gateway/proxy.go` (`doHTTP` at L472+, external-MCP variant at L770+) and `server/internal/toolconfig/toolconfig.go` (`ToolCallEnv.OAuthToken`).

## Related skills

- `mcp-servers-and-endpoints` — the parallel internal RFC about MCP servers and endpoints that the audience semantics for the new client-session JWT depends on.
- `gram-rbac` — for `urn.Principal`, scope enforcement, and how the new `PrincipalTypeAnonymous` will land.
- `gram-management-api` — for adding the `client_session_issuer` / `remote_oauth_issuer` / `remote_oauth_client` management endpoints.
- `postgresql` — for migrations as the legacy tables are split and superseded.
