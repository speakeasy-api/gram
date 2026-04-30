# Spike: Remote OAuth Clients for Private Repos

> Reference implementation lives on this branch (`private-oauth-toolsets-SwkL`). **Not for merge.** The branch exists to plan the delta in detail and to seed follow-on PRs per `project.md`.
>
> Reviewer convention: leave inline feedback as `> [FIX]`, `> [Q]`, `> [REMOVE]`, `> [???]`, `> [TBD]`, `> [DROP]` blockquotes next to the offending line or above a section. One concern per tag.

## 1. Overview

We currently have a product need to secure OAuth servers with a Gram login, but we are presented with a problem: securing servers with Gram login uses the same method as securing credentials for upstream OAuth providers with some special behavior applied. In order to remove this product constraint, instead of relaxing the constraint on allowing a single vs multiple upstream OAuth providers, we will instead allow securing the sessions that upstream OAuth tokens are stored keyed by sessions that are allowed to be an authenticated resource.

Our solution is to decouple the `oauth` package into two packages:

1. _clientsessions_: allow Gram to act as an authorization server for MCP clients and resolve identities to either anonymous sessions or Gram principals
2. _remotesessions_: functionality where Gram acts as a Client for remote MCP Servers

We take the opportunity to solve many ongoing design challenges:

1. Store remote OAuth credentials as their own documents keyed on session rather than as properties of the client session document
2. Resolve language overloading and unify the concepts of `external_oauth_providers` and `oauth_proxy_providers`
3. Reduce the number of discrete authentication pathways on the `/mcp` endpoint
4. Allow multiple remote OAuth providers for a single MCP session
5. Make stronger guarantees of consent collection for each user

We leave out of scope: Playground OAuth (i.e. settings where Gram acts as an MCP OAuth Client rather than an `issuer`) and tampering with Gram management API sessions.

## 2. Definitions

This section is the canonical glossary for the rest of the spike. Section 2a is a refresher of the OAuth terms we use unmodified; section 2b defines the Gram-specific terms this RFC introduces. When a Gram term overlaps with a generic OAuth term, the Gram definition wins inside this codebase.

### 2a. General OAuth terms (refresher)

| Term                                  | Meaning in this document                                                                                                                                                                                               |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Authorization Server (AS)**         | Issues access and refresh tokens; runs `/authorize`, `/token`, and (when applicable) `/register`. Synonyms in the wild: _issuer_, _OAuth provider_, _identity provider_ (when the AS also speaks OIDC). RFC 6749 §1.1. |
| **Resource Server (RS)**              | Hosts protected resources; validates access tokens presented by clients. Synonyms: _protected resource_ (RFC 9728), _audience_ (when referred to by what a token's `aud` claim binds). RFC 6749 §1.1.                  |
| **Client**                            | The application that requests access on behalf of a user. Synonyms: _application_, _relying party_ (in OIDC). RFC 6749 §1.1.                                                                                           |
| **OIDC (OpenID Connect)**             | A protocol layered on OAuth that mandates a particular OAuth flow to enable external providers to solve authentication challenges. We adopt OIDC's JWT _schema_ but not its mandated public-key signing.               |
| **DCR (Dynamic Client Registration)** | A client registers itself with an AS via `/register`. RFC 7591.                                                                                                                                                        |
| **PKCE**                              | Public-client protection on the authorization-code grant via `code_verifier` / `code_challenge` (S256 only in this codebase). RFC 7636.                                                                                |

### 2b. Gram-specific terms

The following terms are introduced or redefined by this RFC. When the codebase still has legacy structures with overlapping names, the legacy term is called out explicitly. **Reviewers should take special care to comment and align on these terms.**

#### Client session

The session Gram maintains with an MCP client. A client session has exactly one principal (`user`, `apikey`, or `anonymous`), is bound to exactly one toolset, and is materialised as:

- A signed access token
- A Redis-backed refresh document, keyed `(session_id, client_session_issuer_id)`

This RFC also deprecates the legacy `Gram-Chat-Session` header. Chat-session JWTs become `Authorization`-header-delivered tokens that share the same JWT schema and signing key as client sessions; the two flows unify under one claim shape and one revocation path, differing only in `sub` and `aud`.

#### Client session issuer (`client_session_issuer`)

The Gram-side authorization-server configuration that issues client sessions for a toolset. Replaces today's `oauth_proxy_servers` row. A toolset that wants to gate MCP traffic with a Gram-issued session points at a `client_session_issuer`. A `client_session_issuer` may reference zero or more `remote_oauth_issuer`s — i.e. there can be multiple remote OAuth challenges to satisfy on the way to issuing a client session. (See _implicit_ vs _interactive_ below for how those challenges are presented.)

#### Remote OAuth issuer (`remote_oauth_issuer`)

An upstream Authorization Server's identity record. Holds AS metadata (RFC 8414 fields), the issuer URL, and a required `oidc bool` flag (defaulted `false`) that may unlock OIDC-aware behaviour when `true`. Issuer rows can be auto-discovered (e.g. by hitting an upstream `/.well-known/oauth-authorization-server`) and are managed independently of the credentials Gram presents to the issuer.

Conceptually this is closer to today's `oauth_proxy_provider` than to `external_oauth_server_metadata`. The behavioural difference between the two legacy modes collapses onto a single `passthrough` flag on the issuer.

#### Remote OAuth client (`remote_oauth_client`)

Credentials Gram uses when acting as a client of a `remote_oauth_issuer`. One issuer can have many clients in the schema; in initial scope we use 1:1, but the structure leaves room for, e.g., multiple Notion-app credentials against a single shared Notion MCP. This serves as a jump table between `oauth_proxy_providers` and `client_session_issuers` — i.e. each `remote_oauth_client` should have its own `client_session_issuer` associated. We would likely be better served to have a single `remote_oauth_client` for each provider that can be shared across all of Gram, but we leave the definition of a permission model for client IDs and secrets (which can be customer-provided) out of scope here.

#### Passthrough mode (on a `remote_oauth_issuer`)

Mode where the bearer the MCP client sent us is forwarded to the upstream as-is, rather than Gram exchanging for an upstream token of its own. We still conform to the abstractions — if storing a remote session document is what homogeneity requires, we store one. Notably, the MCP client will register a client directly with the remote Authorization Server rather than with Gram. The access token will be delivered directly to that authorization server as it is received by the client.

This is the **same concept** as Milestone #2's _passthrough authentication_. The two names are aliases.

#### Implicit vs Interactive (modes on a `client_session_issuer`)

Both modes describe how multi-remote OAuth challenges are presented after Gram issues a client session.

- **Implicit.** From the Gram callback, redirect through each subsequent remote challenge in turn, build the entire session, then redirect back to the MCP client callback for the final token exchange. There is no intermediate UI listing the remotes. Consent must still be prompted _somewhere_ in the request stream — implicit mode does not skip consent, it just doesn't render a "click each server" screen.
- **Interactive.** Gram issues the client session up front, then renders a UX where the user clicks each remote OAuth server to authenticate. This is the same screen that Milestone #8's URL-mode elicitation points at when refreshing stale remote credentials.
- **Just-In-Time** (out of scope). We only offer the challenge for tokens that the client is trying to use for _this_ given request. Similar behavior to _implicit_ but more aggressive.

#### Anonymous principal

A principal with no authenticated identity. Provisioned **only** through the MCP pathway. Materialised as a `PrincipalType` joining the existing `user` and `role` types in `urn.Principal`, with the URN shape `anonymous:<mcp-session-id>`. The `mcp-session-id` segment is the same value the MCP handler injects into the `client_session_issuer` per goal #11.

`role:<slug>` is **not** a valid `sub` value — roles are not authentication subjects. The valid `sub` URN shapes are exactly `user:<id>`, `apikey:<uuid>`, and `anonymous:<mcp-session-id>`. `urn.APIKey` stays a parallel URN kind (it is not a `PrincipalType`) — this RFC keeps that separation but allows both PrincipalType URNs and APIKey URNs to share the `sub` claim.

#### Consent record

Persistent record that a given user has consented for a given `client_session_issuer` to access **all** of its `remote_session_tokens`. The `/authorize` endpoint may skip the consent prompt only when a matching consent record exists. Existence of a session alone is not sufficient. (Where this record lives — Postgres table vs. another Redis doc — is a §4 design question.)

---

## 3. Architecture overview

### 3.1 Layout

Today's `server/internal/oauth/` is replaced by two qualified packages:

- **`clientsessions/`** — Gram-as-Authorization-Server. AS endpoints, client-session JWT issuance + refresh, principal resolution, consent enforcement.
- **`remotesessions/`** — Gram-as-OAuth-Client. The dance with each `remote_oauth_issuer` (discovery, code exchange, token storage, refresh) and presentation of materialised credentials.

`server/internal/mcp/` orchestrates both. A new `mcp/challenge.go` takes errors from `clientsessions` and `remotesessions` and coerces them into the right shape for MCP auth challenges.

There is no homogeneous dependency story between these packages. `mcp/` will know both. `clientsessions` will sometimes need remote-session state to complete identity resolution (notably around passthrough remote-token issuers). `client_session_issuer` configurations will sometimes need to know which backends they relate to. We won't enforce a hard one-direction rule; we'll keep each dependency as narrow as the implementation actually requires and revisit if any single direction starts carrying real weight.

### 3.2 Who owns what

| Concept                                               | Lives in                                                                          |
| ----------------------------------------------------- | --------------------------------------------------------------------------------- |
| `client_session_issuer` (config + JWT issuance)       | `clientsessions/`                                                                 |
| Consent records, consent-prompt logic                 | `clientsessions/`                                                                 |
| Anonymous principal provisioning (MCP-only)           | `clientsessions/`                                                                 |
| `remote_oauth_issuer`, `remote_oauth_client` (config) | `remotesessions/`                                                                 |
| Remote session documents (Redis)                      | `remotesessions/`                                                                 |
| Passthrough mode (the "no exchange" code path)        | `remotesessions/`                                                                 |
| Implicit / Interactive challenge orchestration        | `mcp/` (orchestrates), `clientsessions/` + `remotesessions/` (execute primitives) |
| MCP auth challenge translation                        | `mcp/challenge.go`                                                                |
| Tool-call credential injection                        | Resolved by `mcp/`; presented to the backend as opaque inputs                     |

### 3.3 The MCP request — orchestration

`server/internal/mcp/` orchestrates each request in four steps:

1. **Resolve the credential to an identity.** Call `clientsessions` to validate whatever the client presented (Gram-issued JWT, or nothing — in which case the supplied `mcp_session_id` becomes the id of an anonymous session). Returns a principal.
2. **Authorize server access.** Check whether that principal is permitted to use this toolset.
3. **Request credentials from `remotesessions` for this identity.** Materialise (or refresh) every required remote session keyed on `(session_id, client_session_issuer_id)`. If anything is missing or stale, fire the appropriate challenge (implicit / interactive / passthrough).
4. **Pass credentials to the MCP backend.** The toolset receives an opaque credential bundle. `doHTTP` consumes it without any OAuth knowledge — satisfying goal #7.

### 3.4 Where consent fires

Consent enforcement lives entirely in `clientsessions` at the `/authorize` endpoint. The check is: "has this user previously consented for this `client_session_issuer` to access **all** of its `remote_session_tokens`?" Only a matching consent record short-circuits the prompt. Re-prompting is the default whenever the set of `remote_oauth_issuer`s on the issuer changes (i.e. consent is bound to the _full set_, not to individual remotes).

In implicit mode, the prompt fires somewhere in the redirect chain — typically before the first remote challenge. In interactive mode, the prompt is folded into the per-remote click-through UX.

### 3.5 Relationship to the MCP server / backend split

Everything in this RFC — `client_session_issuer`, `remote_oauth_issuer`, `remote_oauth_client`, consent records, every Redis session document — attaches at the **`mcp_servers`** level (Gram's MCP-server configuration), not on the backend. The MCP backend (a `toolsets` row or a `remote_mcp_servers` row) only consumes the credential bundle Gram has already assembled; it has no OAuth knowledge. Backends accept credentials; collecting them is the MCP server's job.

This RFC does not require finishing the MCP runtime's migration off the legacy `toolsets.{external_oauth_server_id, oauth_proxy_server_id}` columns. We attach `client_session_issuer` at whichever level (`toolsets` row or `mcp_servers` row) is current at implementation time and migrate alongside whatever `mcp_endpoints` work is happening in parallel.

---

## 4. Schemas

Canonical SDL lives in `/schemas/` (Postgres in `.sql`, Redis JSON in Go SDL, JWT in Go SDL). This section is the index and rationale; precise column types and indexes are in those files.

### 4.1 Postgres — new tables

#### `client_session_issuers`

The Gram-side AS configuration. One per logical "thing that issues client sessions for a toolset."

Open Questions:

- Should the issuer be able to force authentication? Or should it always defer to the policy set by the MCP server (i.e. the `mcp_servers` row)? Will attempt implementing the latter and fix if it falls over

| Column           | Type   | Notes                                               |
| ---------------- | ------ | --------------------------------------------------- |
| `id`             | `uuid` | PK                                                  |
| `project_id`     | `uuid` | FK → `projects`                                     |
| `slug`           | `text` | unique per project                                  |
| `challenge_mode` | `text` | `implicit` \| `interactive`                         |
| timestamps       |        | `created_at`, `updated_at`, `deleted_at`, `deleted` |

#### `remote_oauth_issuers`

An upstream Authorization Server's identity record. Successor to `oauth_proxy_provider` (per §2b: behavioural diff from `external_oauth_server_metadata` collapses onto the `passthrough` flag).

| Column                                  | Type     | Notes                                                                                                                   |
| --------------------------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------- | --- |
| `id`                                    | `uuid`   | PK                                                                                                                      |
| `project_id`                            | `uuid`   | FK → `projects`; should ultimately not be project scoped and be able to share working configurations across remotes     |     |
| `slug`                                  | `text`   | unique per project                                                                                                      |
| `issuer`                                | `text`   | issuer URL, matches `iss` claim                                                                                         |
| `authorization_endpoint`                | `text`   |                                                                                                                         |
| `token_endpoint`                        | `text`   |                                                                                                                         |
| `registration_endpoint`                 | `text`   | nullable; absent for issuers without DCR                                                                                |
| `jwks_uri`                              | `text`   | nullable                                                                                                                |
| `scopes_supported`                      | `text[]` |                                                                                                                         |
| `grant_types_supported`                 | `text[]` |                                                                                                                         |
| `response_types_supported`              | `text[]` |                                                                                                                         |
| `token_endpoint_auth_methods_supported` | `text[]` |                                                                                                                         |
| `oidc`                                  | `bool`   | default `false`; `true` may unlock OIDC-aware behaviour                                                                 |
| `passthrough`                           | `bool`   | default `false`; when `true`, the MCP client registers + transacts directly with this issuer (per §2b passthrough mode) |
| timestamps                              |          |                                                                                                                         |

#### `remote_oauth_clients`

The credentials Gram presents when transacting against a `remote_oauth_issuer`. Jump-table edge between `remote_oauth_issuer` and `client_session_issuer` (per §2b).

| Column                     | Type          | Notes                                 |
| -------------------------- | ------------- | ------------------------------------- |
| `id`                       | `uuid`        | PK                                    |
| `project_id`               | `uuid`        | FK → `projects`                       |
| `remote_oauth_issuer_id`   | `uuid`        | FK                                    |
| `client_session_issuer_id` | `uuid`        | FK; the issuer this client maps to;   |
| `client_id`                | `text`        |                                       |
| `client_secret_encrypted`  | `text`        | nullable for PKCE-only public clients |
| `client_id_issued_at`      | `timestamptz` |                                       |
| `client_secret_expires_at` | `timestamptz` | nullable for non-expiring secrets     |
| timestamps                 |               |                                       |

#### `client_session_consents`

Persistent consent record per (user, `client_session_issuer`).

| Column                     | Type          | Notes                                                                                   |
| -------------------------- | ------------- | --------------------------------------------------------------------------------------- |
| `id`                       | `uuid`        | PK                                                                                      |
| `user_id`                  | `text`        | the consenting user (NOT the principal URN; consent is user-scoped, not session-scoped) |
| `client_session_issuer_id` | `uuid`        | FK                                                                                      |
| `remote_set_hash`          | `text`        | SHA-256 of the sorted list of `remote_oauth_issuer_id`s on the issuer at consent time   |
| `consented_at`             | `timestamptz` |                                                                                         |
| timestamps                 |               |                                                                                         |

#### Toolset link

`toolsets` gains:

| Column                     | Type   | Notes                                                                                                                |
| -------------------------- | ------ | -------------------------------------------------------------------------------------------------------------------- |
| `client_session_issuer_id` | `uuid` | FK → `client_session_issuers`; nullable; replaces both legacy `external_oauth_server_id` and `oauth_proxy_server_id` |

(`mcp_servers` mirrors this column whenever its runtime migration lands, per §3.5.)

### 4.2 Postgres — removed

| What                                           | Why                                                                                                                                                                                   |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `oauth_proxy_servers`                          | Replaced by `client_session_issuers`.                                                                                                                                                 |
| `oauth_proxy_providers`                        | Replaced by `remote_oauth_issuers` + `remote_oauth_clients`.                                                                                                                          |
| `oauth_proxy_providers.secrets` JSONB          | Deprecated per goal #3; structured columns on `remote_oauth_clients` instead.                                                                                                         |
| `oauth_proxy_providers.security_key_names`     | Deprecated per goal #3.                                                                                                                                                               |
| `oauth_proxy_providers.provider_type='custom'` | Deprecated per goal #3; behaviour collapses onto `passthrough` flag.                                                                                                                  |
| `external_oauth_server_metadata`               | Use case preserved as `remote_oauth_issuer` with `passthrough=true`.                                                                                                                  |
| `toolsets.external_oauth_server_id`            | Replaced by `toolsets.client_session_issuer_id`.                                                                                                                                      |
| `toolsets.oauth_proxy_server_id`               | Replaced by `toolsets.client_session_issuer_id`.                                                                                                                                      |
| `toolsets_oauth_exclusivity` CHECK             | No longer needed; only one column to set.                                                                                                                                             |
| `oauth_proxy_client_info`                      | **TBD — see open questions.** This is the DCR registry for MCP clients registering with Gram-as-AS. Likely renamed to `client_session_dcr_registrations` to match the new vocabulary. |

Each removal becomes a ticket in `project.md`.

### 4.3 Redis — new types

All implement `cache.CacheableObject[T]`; values JSON-serialised by `cache.TypedCacheObject[T]`. Encrypted fields use `encryption.Client` before serialisation. Where a key segment shows `{principalURN}`, the URN is `user:<id>` | `apikey:<uuid>` | `anonymous:<mcp-session-id>`.

| Type                                 | Cache key                                                                    | Holds                                                                                                               | TTL                            |
| ------------------------------------ | ---------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- | ------------------------------ |
| `ClientSession`                      | `clientSession:{refreshTokenHash}`                                           | principal URN, `client_session_issuer_id`, jti, refresh expiry                                                      | `time.Until(RefreshExpiresAt)` |
| `ClientSessionIndex`                 | `clientSessionByPrincipal:{principalURN}:{clientSessionIssuerID}`            | bookkeeping; list of active refresh-token hashes for this principal at this issuer (revoke-all, "who's logged in?") | `time.Until(RefreshExpiresAt)` |
| `RemoteSession`                      | `remoteSession:{principalURN}:{clientSessionIssuerID}:{remoteOAuthIssuerID}` | access token (enc), refresh token (enc), **separate** access/refresh expiries, scopes, `remote_oauth_client_id`     | `time.Until(RefreshExpiresAt)` |
| `ClientSessionGrant`                 | `clientSessionGrant:{clientSessionIssuerID}:{code}`                          | `client_id`, redirect_uri, scope, state, PKCE challenge, principal URN                                              | ~10 min                        |
| `RemoteSessionAuthState`             | `remoteSessionAuthState:{stateID}`                                           | principal URN, issuer id, client id, code verifier, redirect                                                        | ~10 min                        |
| `RemoteSessionPKCE`                  | `remoteSessionPKCE:{nonce}`                                                  | verifier                                                                                                            | 10 min fixed                   |
| `RevokedToken` _(unchanged, reused)_ | `chat_session_revoked:{jti}`                                                 | jti, revoked_at                                                                                                     | 24h                            |

Two design notes informing this shape:

- **`ClientSession` is keyed by the refresh-token hash.** That's what `/token` queries when exchanging a refresh token; it's the hot path. We don't key by session id because the operation that needs the lookup is "give me the session for this presented refresh token".
- **`ClientSessionIndex` is the bookkeeping reverse-index.** It answers "what active sessions does this principal have at this issuer?" — needed for revoke-all, listing, and operational queries. Keyed on `principalURN` rather than a separate session-id concept; for anonymous principals the `anonymous:<mcp-session-id>` URN itself encodes the session id, so no separate id is required. Authenticated principals (`user:<id>`, `apikey:<uuid>`) are looked up directly by URN.

### 4.4 Redis — removed

| What                                           | Why                                                                                                                                 |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `oauthGrant:{toolsetID}:{code}`                | Replaced by `clientSessionGrant:*` keyed on `client_session_issuer_id`.                                                             |
| `oauthToken:{toolsetID}:{accessToken}`         | Eliminated. Access tokens are validated as JWTs (no Redis read on the validate path).                                               |
| `oauthRefreshToken:{toolsetID}:{refreshToken}` | Replaced by `clientSession:*` keyed on `(session_id, client_session_issuer_id)`.                                                    |
| `oauthClientInfo:{mcpURL}:{clientID}`          | TBD — pairs with the `oauth_proxy_client_info` table decision.                                                                      |
| `upstreamPKCE:{nonce}`                         | Renamed `remoteSessionPKCE:*`.                                                                                                      |
| `externalOAuthState:{stateID}`                 | Renamed `remoteSessionAuthState:*`.                                                                                                 |
| `Token.ExternalSecrets` (sub-field)            | The whole "tunnel upstream credentials through the AS token" pattern is gone. Remote credentials live in `RemoteSession` documents. |

### 4.5 JWT — unified `SessionClaims`

One claim shape for chat sessions and client sessions. Same signing key (`GRAM_JWT_SIGNING_KEY`), same algorithm (HS256), same revocation cache (`chat_session_revoked:{jti}`). Differs only in `sub` and `aud`.

```go
type SessionClaims struct {
    // OIDC-shaped registered claims
    Issuer    string   `json:"iss"`           // Gram issuer URL
    Subject   string   `json:"sub"`           // user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>
    Audience  []string `json:"aud"`           // toolset slug (client session) | embed origin (chat session)
    ExpiresAt int64    `json:"exp"`
    IssuedAt  int64    `json:"iat"`
    JTI       string   `json:"jti"`           // UUIDv4

    // Gram-specific
    OrgID            string  `json:"org_id"`
    ProjectID        string  `json:"project_id"`
    OrganizationSlug string  `json:"organization_slug"`
    ProjectSlug      string  `json:"project_slug"`
    ExternalUserID   *string `json:"external_user_id,omitempty"` // chat-session only
}
```

Notes:

- No more `api_key_id` claim — it's encoded in `sub` (`apikey:<uuid>`) when applicable.
- `Subject` MUST be one of the three valid URN shapes; `role:<slug>` is rejected at sign time.
- `Audience` is the seam validators use to refuse a chat-session JWT presented as a client-session token (and vice versa).
- Tokens are delivered via the standard `Authorization: Bearer <token>` header. The legacy `Gram-Chat-Session` header is deprecated.

### 4.6 SDL artifacts

The schemas above are the rationale; the canonical artifacts live in `/schemas/`:

- `/schemas/postgres.sql` — DDL for the new tables and the toolset alteration.
- `/schemas/redis.go` — Go-SDL definitions for each Redis type.
- `/schemas/jwt.go` — `SessionClaims` and the valid `sub` URN shapes.

The legacy state (the `oauth_proxy_*` / `external_oauth_server_metadata` / legacy-`Token` shape) is captured for reference in the `gram-legacy-oauth` skill draft.

---

## 5. Flows

This section captures the flows we plan to ship: the per-request state machine that orchestrates a single MCP call (§5.1), and the two challenge sequences that establish a client session in the first place (§5.2 implicit, §5.3 interactive). Per §3.5, all of this is MCP-server-level — the backend never sees an OAuth token.

We deliberately don't break out separate `client-session-challenge` and `remote-session-challenge` diagrams. The interesting design lives in the unified flow; per-component sub-flows would hide that. If either of the unified diagrams gets too dense we'll factor.

> Backend errors and their relationship to OAuth are **out of scope** for this section. The state machine in §5.1 treats backend invocation as a single transition; how a backend's failure response surfaces to the MCP client is a separate concern.

Diagrams are sources of truth in `/diagrams/*.mermaid`; sections below link rather than embed. Open the file in any mermaid renderer to view.

### 5.1 `mcp-handler` — state machine

The per-request lifecycle. Fires _every_ time a request lands on `/mcp/{slug}`, separately from the one-time challenge flow that gets the client a JWT in the first place. The challenge flow (§5.2/§5.3) is what a client runs in response to one of the terminal challenge states below.

**Diagram:** [`diagrams/mcp-handler-states.mermaid`](diagrams/mcp-handler-states.mermaid)

State summary:

- **PolicyGate.** First check: does this server have a Gram authorization policy? If no policy (public server), skip identity + authz and head straight for credential collection. If a policy exists, the server is private — and **private always implies RBAC**.
- **ResolvingPrincipal.** `clientsessions/` validates the `Authorization: Bearer <JWT>` _and_ resolves whether the principal still exists. The `sub` is one of `user:<id>`, `apikey:<uuid>`, or `anonymous:<mcp-session-id>`. Anonymous principals are _not_ provisioned at request time — they are minted only during a §5.2 / §5.3 challenge. If the JWT is invalid OR the principal can't be resolved, `ClientSessionManager` raises a `ChallengeRequiredError`.
- **Authorizing.** RBAC scope check. Failure raises `Forbidden`.
- **CollectingRemoteCredentials.** `remotesessions/` ensures every required `RemoteSession` for `(principal, remote_oauth_client)` is current. Stale-but-refreshable sessions are silently refreshed inline. A session that's missing or expired beyond refresh causes `RemoteSessionManager` to raise a `ChallengeRequiredError`.
- **MergingCredentials.** Resolved OAuth credentials are merged with the toolset's static environment. The merged bundle is what `DispatchToBackend` hands to the backend.
- **DispatchToBackend.** Backend invoked with the credential bundle. Backend errors and their interaction with OAuth are **out of scope** for this section (see §7).
- **Terminal states:**
  - `Success` — 200 with the backend's response.
  - `ClientSessionChallenge` — `ClientSessionManager` raised `ChallengeRequiredError`. The handler coerces this to a 401 + `WWW-Authenticate` pointing at `/mcp/authorize`. Client runs §5.2 / §5.3 to acquire a JWT, then retries.
  - `RemoteSessionChallenge` — `RemoteSessionManager` raised `ChallengeRequiredError`. The handler coerces this to a URL-mode elicitation pointing at `/mcp/connect` (Milestone #8) — otherwise falls back to a 401 auth challenge identical to `ClientSessionChallenge`.
  - `Forbidden` — 403. Not a challenge — no client action will fix it.

The two `ChallengeRequiredError` paths are the only mechanism by which the per-request handler triggers the challenge flow. The handler's coercion logic — `ClientSessionManager` → 401, `RemoteSessionManager` → URL-elicitation-or-401 — is what `mcp/challenge.go` (§3.1) is for.

### 5.2 `unified-challenge` — implicit mode

**Pre-condition.** The toolset is configured with a single `client_session_issuer` whose `remote_set` contains one `remote_oauth_issuer` pointing at Linear:

| Field                    | Value                                      |
| ------------------------ | ------------------------------------------ |
| `issuer`                 | `https://login.linear.com`                 |
| `authorization_endpoint` | `https://login.linear.com/oauth/authorize` |
| `token_endpoint`         | `https://login.linear.com/oauth/token`     |
| `oidc`                   | `false`                                    |
| `passthrough`            | `false`                                    |

A `remote_oauth_client` registered with Linear (`client_id` + `client_secret_encrypted`) is already on file. In a multi-remote configuration, Phase 3 would loop once per `remote_oauth_client`; this example shows a single iteration.

Gram exposes the following URLs in this flow. Each callback handler re-loads ChallengeState by `state` and runs `buildRequiredChallenge` to decide what 302 comes next.

| Path                                               | Method     | Role                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| -------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `https://app.getgram.ai/mcp/authorize`             | GET        | AS authorize endpoint (entry — sets up ChallengeState)                                                                                                                                                                                                                                                                                                                                                                                                      |
| `https://app.getgram.ai/mcp/client_login_callback` | GET        | Speakeasy returns here after Gram login (`clientsessions/`)                                                                                                                                                                                                                                                                                                                                                                                                 |
| `https://app.getgram.ai/mcp/remote_login_callback` | GET        | Remote AS returns here after upstream login (`remotesessions/`)                                                                                                                                                                                                                                                                                                                                                                                             |
| `https://app.getgram.ai/mcp/connect`               | GET / POST | Connect UI (`clientsessions/`) — GET renders the per-remote + grant-access page; POST disambiguates from the form body alone — a `remote_oauth_client` selector means "start that remote's auth flow", absence means "Give Access click" (server runs `buildRequiredChallenge` to confirm all remotes are connected before minting the grant). In implicit mode only the no-selector path is exercised, since remotes are completed via the redirect chain. |
| `https://app.getgram.ai/mcp/token`                 | POST       | AS token endpoint (`clientsessions/`)                                                                                                                                                                                                                                                                                                                                                                                                                       |

**Diagrams.**

- Sequence (actor view — MCP Client, Browser, `clientsessions/`, `remotesessions/`, Speakeasy, Linear): [`diagrams/unified-challenge-implicit.mermaid`](diagrams/unified-challenge-implicit.mermaid)
- State machine (lifecycle view — abstracts the auth handshakes; covers both implicit and interactive modes; left-to-right): [`diagrams/unified-challenge-states.mermaid`](diagrams/unified-challenge-states.mermaid)

Design notes:

- **ChallengeState is the through-line.** Phase 1 writes a thin Redis doc holding the MCP Client's OAuth request context. Each subsequent callback re-loads it by handle. `buildRequiredChallenge` is **idempotent** — every step runs it again, observing live state (RemoteSession docs, consent records) to decide the next 302. We deliberately don't accumulate progress markers on ChallengeState; the existence of a `RemoteSession(principal, remote_oauth_client)` IS the completion marker.
- **Phase 2 reads identity level from ChallengeState** (`authenticated` | `anonymous`) and three-way branches: reuse the principal, issue an anonymous session, or run the Speakeasy login chain. Speakeasy is today's upstream IDP — wrapped in `activate`/`deactivate` to mark the dependency. Open: register Gram MCP Gateway as a true OIDC app so we drop the Speakeasy-as-IDP middleman.
- **Phase 3 short-circuits on `ensure session`.** If `remotesessions/` finds a current `RemoteSession` (or can silently refresh), no redirect. The Linear dance only runs when the session is missing or expired. RemoteSessions are scoped by `(principal, remote_oauth_client)` — not by MCP server. First-iteration constraints: one MCP server per `remote_oauth_client`, one issuer per `remote_oauth_client` (so the client implies its issuer); both may relax later.
- **Phase 4 mints `ClientSessionGrant` only after consent**, then redirects to the MCP Client's `redirect_uri`. The `ClientSession` Redis doc is created **lazily at token exchange** — minting a JWT and storing a refresh-token hash should not happen until the MCP Client actually exchanges the code.
- **Two real callback URLs, plus a unified connect UI.** `/mcp/client_login_callback` (clientsessions, Speakeasy returns here) and `/mcp/remote_login_callback` (remotesessions, remote ASes return here) are the OAuth callbacks. `/mcp/connect` is shared with §5.3 interactive — it is the page where the user grants access (and, in interactive mode, drives per-remote connections). Each handler runs `buildRequiredChallenge` and 302s to the next required step. No `?finalize=` query-param hack.
- **ChallengeState contents are deliberately unpinned here.** At minimum: the MCP Client's OAuth request fields and (after Phase 2) the resolved principal URN. Schema lands in §4 once flows are stable.
- **Browser absorbs the user.** No separate `User` lane.

#### Parameter reference

The opaque-looking `state`, `code`, and `verifier` parameters in the sequence diagram each map to specific records. This table tracks what's in each:

| Param                                                          | Where it appears | Contents / Redis reference                                                                                                                      |
| -------------------------------------------------------------- | ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `state` (MCP Client → `/mcp/authorize`)                        | URL query        | MCP Client's anti-CSRF state — echoed verbatim in Phase 4's redirect to `redirect_uri`                                                          |
| `code_challenge` (MCP Client → `/mcp/authorize`)               | URL query        | MCP Client's PKCE challenge — kept on ChallengeState, then on `ClientSessionGrant`                                                              |
| `return_url` (Gram → Speakeasy `/v1/speakeasy_provider/login`) | URL query        | `https://app.getgram.ai/mcp/client_login_callback`                                                                                              |
| `state` (Gram → Speakeasy `/v1/speakeasy_provider/login`)      | URL query        | ChallengeState id                                                                                                                               |
| `code` (Speakeasy → `/mcp/client_login_callback`)              | URL query        | Speakeasy-issued one-time auth code (consumed in Phase 2)                                                                                       |
| `state` (Speakeasy → `/mcp/client_login_callback`)             | URL query        | echoed ChallengeState id                                                                                                                        |
| `state` (Gram → Linear `/oauth/authorize`)                     | URL query        | `RemoteSessionAuthState.StateID` → Redis `remoteSessionAuthState:{stateID}`                                                                     |
| `code_challenge` (Gram → Linear `/oauth/authorize`)            | URL query        | hash of `RemoteSessionPKCE.Verifier` → Redis `remoteSessionPKCE:{nonce}` (nonce encoded in `state`)                                             |
| `code` (Linear → `/mcp/remote_login_callback`)                 | URL query        | Linear-issued one-time auth code (consumed in Phase 3)                                                                                          |
| `state` (Linear → `/mcp/remote_login_callback`)                | URL query        | echoed `RemoteSessionAuthState.StateID`                                                                                                         |
| `verifier` (Gram → Linear `/oauth/token`)                      | request body     | `RemoteSessionPKCE.Verifier`                                                                                                                    |
| `state` (`/mcp/remote_login_callback` → `/mcp/connect`)        | URL query        | ChallengeState id                                                                                                                               |
| `state` (`/mcp/connect` GET ↔ POST)                            | URL query        | ChallengeState id (same value passed through render → submit)                                                                                   |
| `remote_oauth_client` (`/mcp/connect` POST body, optional)     | form field       | when present, the id of the remote whose Connect button was clicked — server starts that remote's auth flow. Absent on Give Access submissions. |
| `code` (Gram → MCP Client `redirect_uri`)                      | URL query        | `ClientSessionGrant.Code` → Redis `clientSessionGrant:{clientSessionIssuerID}:{code}`                                                           |
| `state` (Gram → MCP Client `redirect_uri`)                     | URL query        | echoed MCP Client's original `state` — read from ChallengeState                                                                                 |
| `verifier` (MCP Client → `/mcp/token`)                         | request body     | MCP Client's PKCE verifier — matches `code_challenge` from Phase 1                                                                              |
| `access_token` (Phase 4 response)                              | response body    | freshly-minted `SessionClaims` JWT — no Redis row                                                                                               |
| `refresh_token` (Phase 4 response)                             | response body    | opaque token — SHA-256 hash keys `ClientSession` and appears in `ClientSessionIndex.ActiveRefreshHashes`                                        |

### 5.3 `unified-challenge` — interactive mode

_[To draft after §5.2 is locked.]_

---

_[Sections 6–8 to be drafted next pass — Management API surface, Out of scope, Open questions.]_
