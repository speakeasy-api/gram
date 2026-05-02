# Spike: Remote OAuth Clients for Private Repos

> Reference implementation lives on this branch (`private-oauth-toolsets-SwkL`). **Not for merge.** The branch exists to plan the delta in detail and to seed follow-on PRs per `project.md`.
>
> Reviewer convention: leave inline feedback as `> [FIX]`, `> [Q]`, `> [REMOVE]`, `> [???]`, `> [TBD]`, `> [DROP]` blockquotes next to the offending line or above a section. One concern per tag.

## 1. Overview

We currently have a product need to secure OAuth servers with a Gram login, but we are presented with a problem: securing servers with Gram login uses the same method as securing credentials for upstream OAuth providers with some special behavior applied.

In order to remove this product constraint, we will introduce a new concept of **Remote Sessions** that a Gram user can own. These **Remote Sessions** can then be accessed on behalf of a Gram user in the gateway. They will be secured by a new **User Sessions** manager that is coordinated by the MCP package.

We will recompose the `oauth` package into two packages:

1. _usersessions_: allow Gram to act as an authorization server for MCP clients and resolve identities to either anonymous sessions or Gram principals
2. _remotesessions_: functionality where Gram acts as a Client for remote Authorization Servers

We take the opportunity to solve many ongoing design challenges:

1. Store remote OAuth credentials as their own documents keyed on session rather than as properties of the user session document
2. Resolve language overloading and unify the concepts of `external_oauth_providers` and `oauth_proxy_providers`
3. Reduce the number of discrete authentication pathways on the `/mcp` endpoint
4. Allow multiple remote OAuth providers for a single MCP session
5. Make stronger guarantees of consent collection for each user

We leave out of scope: Playground OAuth (i.e. settings where Gram acts as an MCP OAuth Client rather than an `issuer`) and tampering with Gram management API sessions.

## 2. Definitions

This section is the canonical glossary for the rest of the spike. Section 2a is a refresher of the OAuth terms we use unmodified; section 2b defines the Gram-specific terms this RFC introduces. When a Gram term overlaps with a generic OAuth term, the Gram definition wins inside this codebase.

### 2a. OAuth Terms You Should Know to Understand this RFC

#### Authorization Server (AS)

Issues access and refresh tokens; runs `/authorize`, `/token`, and (when applicable) `/register`. Synonyms in the wild: _issuer_, _OAuth provider_, _identity provider_ (when the AS also speaks OIDC). RFC 6749 §1.1.

#### Resource Server (RS)

Hosts protected resources; validates access tokens presented by clients. Synonyms: _protected resource_ (RFC 9728), _audience_ (when referred to by what a token's `aud` claim binds). RFC 6749 §1.1.

#### Client

The application that requests access on behalf of a user. Synonyms: _application_, _relying party_ (in OIDC). RFC 6749 §1.1.

#### OIDC (OpenID Connect)

A protocol layered on OAuth that mandates a particular OAuth flow to enable external providers to solve authentication challenges. We adopt OIDC's JWT _schema_ but not its mandated public-key signing.

#### DCR (Dynamic Client Registration)

A client registers itself with an AS via `/register`. RFC 7591.

### 2b. Gram-specific terms

The following terms are introduced or redefined by this RFC. When the codebase still has legacy structures with overlapping names, the legacy term is called out explicitly. **Reviewers should take special care to comment and align on these terms.**

#### User session

The session Gram maintains with an MCP client. A user session has exactly one principal (`user`, `apikey`, or `anonymous`), is bound to exactly one MCP server, and is materialised as:

- A signed access token
- A Redis-backed refresh document, keyed `(session_id, user_session_issuer_id)`

This RFC also deprecates the legacy `Gram-Chat-Session` header. Chat-session JWTs become `Authorization`-header-delivered tokens that share the same JWT schema and signing key as user sessions; the two flows unify under one claim shape and one revocation path, differing only in `sub` and `aud`.

#### User session issuer (`user_session_issuer`)

The Gram-side authorization-server configuration that issues user sessions for an MCP server. Replaces today's `oauth_proxy_servers` row. An MCP server that wants to gate MCP traffic with a Gram-issued session points at a `user_session_issuer`. A `user_session_issuer` may reference zero or more `remote_session_issuer`s — i.e. there can be multiple remote OAuth authn challenges to satisfy on the way to issuing a user session. (See _chain_ vs _interactive_ below for how those authn challenges are presented.)

This is a generalisation of today's `oauth_proxy_provider` `type='gram'` mode. Today, a server gates either on a Gram login (`type='gram'`) **or** on a custom upstream provider (`type='custom'`), but never both at once on the same server. The new model decouples the gate from the upstream credentials: a `user_session_issuer` describes who can talk to the server (the gate), and zero or more `remote_session_clients` paired with that issuer describe what upstream tokens are attached to those sessions. Pairing a Gram login with one or more upstream OAuth providers on a single server is the capability this RFC unlocks.

A `user_session_issuer` without any remote servers can be particularly useful for making a server private without requiring it to acquire any extra OAuth servers. We should probably invent a scheme to connect **all** private servers to such an issuer by default, but this is not of high priority in our currently prioritized user journeys.

#### Remote OAuth issuer (`remote_session_issuer`)

An upstream Authorization Server's identity record. Holds AS metadata (RFC 8414 fields), the issuer URL, and a required `oidc bool` flag (defaulted `false`) that may unlock OIDC-aware behaviour when `true`. Issuer rows can be auto-discovered (e.g. by hitting an upstream `/.well-known/oauth-authorization-server`) and are managed independently of the credentials Gram presents to the issuer.

Conceptually this is closer to today's `oauth_proxy_provider` than to `external_oauth_server_metadata`. The behavioural difference between the two legacy modes collapses onto a single `passthrough` flag on the issuer.

#### Remote OAuth client (`remote_session_client`)

Credentials Gram uses when acting as a client of a `remote_session_issuer`. One issuer can have many clients in the schema; in initial scope we use 1:1, but the structure leaves room for, e.g., multiple Notion-app credentials against a single shared Notion MCP. This serves as a jump table between `oauth_proxy_providers` and `user_session_issuers` — i.e. each `remote_session_client` should have its own `user_session_issuer` associated. We would likely be better served to have a single `remote_session_client` for each provider that can be shared across all of Gram, but we leave the definition of a permission model for client IDs and secrets (which can be customer-provided) out of scope here.

#### Passthrough mode (on a `remote_session_issuer`)

Mode where the bearer the MCP client sent us is forwarded to the upstream as-is, rather than Gram exchanging for an upstream token of its own. We still conform to the abstractions — if storing a remote session document is what homogeneity requires, we store one. Notably, the MCP client will register a client directly with the remote Authorization Server rather than with Gram. The access token will be delivered directly to that authorization server as it is received by the client.

This is the **same concept** as Milestone #2's _passthrough authentication_. The two names are aliases.

#### Chain vs Interactive (modes on a `user_session_issuer`)

Both modes describe how multi-remote OAuth authn challenges are presented after Gram issues a user session.

- **Chain.** From the Gram callback, redirect through each subsequent remote authn challenge in turn, build the entire session, then redirect back to the MCP client callback for the final token exchange. There is no intermediate UI listing the remotes. Consent must still be prompted _somewhere_ in the request stream — chain mode does not skip consent, it just doesn't render a "click each server" screen.
- **Interactive.** Gram issues the user session up front, then renders a UX where the user clicks each remote OAuth server to authenticate. This is the same screen that Milestone #8's URL-mode elicitation points at when refreshing stale remote credentials.
- **Just-In-Time** (out of scope). We only offer the authn challenge for tokens that the client is trying to use for _this_ given request. Similar behavior to _chain_ but more aggressive.

#### Anonymous principal

The current system allows attaching remote credentials to an arbitrary identity scoped to an MCP session. We introduce a new principal type for this scenario. This principal can never have scopes, but can access credentials for which it has solved an authn challenge.

Materialised as a `PrincipalType` joining the existing `user` and `role` types in `urn.Principal`, with the URN shape `anonymous:<mcp-session-id>`. The `mcp-session-id` segment is the same value the MCP handler injects into the `user_session_issuer` per goal #11.

`role:<slug>` is **not** a valid `sub` value — roles are not authentication subjects. The valid `sub` URN shapes are exactly `user:<id>`, `apikey:<uuid>`, and `anonymous:<mcp-session-id>`. `urn.APIKey` stays a parallel URN kind (it is not a `PrincipalType`) — this RFC keeps that separation but allows both PrincipalType URNs and APIKey URNs to share the `sub` claim.

#### Authn challenge

Any step in our OAuth/login flow where Gram (or a remote IdP) needs the user or client to prove identity — `/mcp/authorize`, the Speakeasy IDP login, a remote OAuth `/authorize` redirect, or a 401 + `WWW-Authenticate` response. We aim to explicitly qualify "challenge" in order to draw the distinction with the new `authz_challenge` concept (the ClickHouse-logged RBAC allow/deny decision; not our concept).

#### Consent record

Persistent record that a given principal has consented for a given `user_session_client` to access **all** of the `remote_session_tokens` on that client's owning `user_session_issuer`. The `/authorize` endpoint may skip the consent prompt only when a matching consent record exists. Existence of a session alone is not sufficient. Consent is **per-client**, not per-issuer — granting consent to MCP client X does not grant consent to MCP client Y registered with the same issuer. (Where this record lives — Postgres table vs. another Redis doc — is a §4 design question.)

---

## 3. Architecture overview

### 3.1 Layout

Today's `server/internal/oauth/` is replaced by two qualified packages:

- **`usersessions/`** — Gram-as-Authorization-Server. AS endpoints, user-session JWT issuance + refresh, principal resolution, consent enforcement.
- **`remotesessions/`** — Gram-as-OAuth-Client. The dance with each `remote_session_issuer` (discovery, code exchange, token storage, refresh) and presentation of materialised credentials.

`server/internal/mcp/` orchestrates the two to perform the dance. A new `mcp/authn_challenge.go` takes errors from `usersessions` and `remotesessions` and coerces them into the right shape for MCP authn challenges and presents a linear story of the entire oauth flow.

### 3.2 Who owns what

| Concept                                                   | Lives in                                                                        |
| --------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `user_session_issuer` (config + JWT issuance)             | `usersessions/`                                                                 |
| Consent records, consent-prompt logic                     | `usersessions/`                                                                 |
| Anonymous principal provisioning (MCP-only)               | `usersessions/`                                                                 |
| `remote_session_issuer`, `remote_session_client` (config) | `remotesessions/`                                                               |
| Remote session documents (Redis)                          | `remotesessions/`                                                               |
| Passthrough mode (the "no exchange" code path)            | `remotesessions/`                                                               |
| Chain / Interactive authn challenge orchestration         | `mcp/` (orchestrates), `usersessions/` + `remotesessions/` (execute primitives) |
| MCP authn challenge translation                           | `mcp/authn_challenge.go`                                                        |
| Tool-call credential injection                            | Resolved by `mcp/`; presented to the backend as opaque inputs                   |

### 3.3 The MCP request — orchestration

`server/internal/mcp/` orchestrates each request in four steps:

1. **Resolve the credential to an identity.** Call `usersessions` to validate whatever the client presented (Gram-issued JWT, or nothing — in which case the supplied `mcp_session_id` becomes the id of an anonymous session). Returns a principal.
2. **Authorize server access.** Check whether that principal is permitted to use this toolset.
3. **Request credentials from `remotesessions` for this identity.** Materialise (or refresh) every required remote session keyed on `(session_id, user_session_issuer_id)`. If anything is missing or stale, fire the appropriate authn challenge (chain / interactive / passthrough).
4. **Pass credentials to the MCP backend.** The toolset receives an opaque credential bundle. `doHTTP` consumes it without any OAuth knowledge — satisfying goal #7.

### 3.4 Where consent fires

Consent enforcement lives entirely in `usersessions` at the `/mcp/connect` endpoint. The check is: "has this principal previously consented for **this MCP client** (i.e. this `user_session_client`) to access **all** of the `remote_session_tokens` on its owning `user_session_issuer`?" Only a matching consent record short-circuits the prompt. Re-prompting is the default whenever the set of `remote_session_issuer`s on the owning issuer changes (i.e. consent is bound to the _full set_, not to individual remotes).

Empty `remote_set` is **not** a special case — the consent record still binds the principal to a specific `remote_set_hash`, even when that hash is the SHA-256 of the empty list. Skipping consent on the empty-set case would let an attacker CSRF the user past consent the moment any `remote_session_issuer` is added to the issuer. The connect page should still render and require an explicit Give Access click.

In both chain and interactive modes the consent prompt is the `/mcp/connect` page — interactive mode renders the per-remote Connect buttons alongside the grant-access button; chain mode lands on the page with all remotes already ✓ and only the grant-access button visible.

### 3.5 Relationship to the MCP server / backend split

Everything in this RFC — `user_session_issuer`, `remote_session_issuer`, `remote_session_client`, consent records, every Redis session document — attaches at the **`mcp_servers`** level (Gram's MCP-server configuration), not on the backend. The MCP backend (a `toolsets` row or a `remote_mcp_servers` row) consumes the credential bundle Gram has assembled — and that bundle absolutely includes upstream OAuth access tokens (e.g., a Linear bearer the backend forwards onto outbound calls). What the backend does **not** do is run the OAuth dance: collecting tokens, refreshing them, prompting for consent. That work stays MCP-server-level.

This RFC does not require finishing the MCP runtime's migration off the legacy `toolsets.{external_oauth_server_id, oauth_proxy_server_id}` columns. We attach `user_session_issuer` at whichever level (`toolsets` row or `mcp_servers` row) is current at implementation time and migrate alongside whatever `mcp_endpoints` work is happening in parallel.

---

## 4. Schemas

Canonical SDL lives in `/schemas/` (Postgres in `.sql`, Redis JSON in Go SDL, JWT in Go SDL). This section is the index and rationale; precise column types and indexes are in those files.

The full Postgres ER diagram is at [`diagrams/schema.mermaid`](diagrams/schema.mermaid).

### 4.1 Postgres — new tables

#### `user_session_issuers`

The Gram-side AS configuration. One per logical "thing that issues user sessions for a toolset."

Open Questions:

- Should the issuer be able to force authentication? Or should it always defer to the policy set by the MCP server (i.e. the `mcp_servers` row)? Will attempt implementing the latter and fix if it falls over

| Column                 | Type       | Notes                                                              |
| ---------------------- | ---------- | ------------------------------------------------------------------ |
| `id`                   | `uuid`     | PK                                                                 |
| `project_id`           | `uuid`     | FK → `projects`                                                    |
| `slug`                 | `text`     | unique per project                                                 |
| `authn_challenge_mode` | `text`     | `chain` \| `interactive`                                           |
| `session_duration`     | `interval` | policy: `user_sessions.expires_at = created_at + session_duration` |
| timestamps             |            | `created_at`, `updated_at`, `deleted_at`, `deleted`                |

#### `user_session_clients`

DCR registry for MCP clients registering with Gram-as-Authorization-Server (RFC 7591). Successor to legacy `oauth_proxy_client_info`. Symmetric counterpart to `remote_session_clients` — same role, opposite side of the AS/Client divide.

Fields dropped relative to legacy: `grant_types`, `response_types`, `scope`, `token_endpoint_auth_method`, `application_type`. These are issuer-level policy that resolves at `/authorize` and `/token` time, not per-client state worth persisting.

| Column                     | Type          | Notes                                                              |
| -------------------------- | ------------- | ------------------------------------------------------------------ |
| `id`                       | `uuid`        | PK                                                                 |
| `user_session_issuer_id`   | `uuid`        | FK                                                                 |
| `client_id`                | `text`        | DCR-issued                                                         |
| `client_secret_hash`       | `text`        | bcrypt or equivalent; nullable for public PKCE clients             |
| `client_name`              | `text`        | from registration request                                          |
| `redirect_uris`            | `text[]`      | validated on every `/authorize`                                    |
| `client_id_issued_at`      | `timestamptz` |                                                                    |
| `client_secret_expires_at` | `timestamptz` | nullable: null = doesn't expire (RFC 7591 `expires_at=0` semantic) |
| timestamps                 |               | unique index on `(user_session_issuer_id, client_id)`              |

#### `remote_session_issuers`

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

#### `remote_session_clients`

The credentials Gram presents when transacting against a `remote_session_issuer`. Jump-table edge between `remote_session_issuer` and `user_session_issuer` (per §2b).

| Column                     | Type          | Notes                                 |
| -------------------------- | ------------- | ------------------------------------- |
| `id`                       | `uuid`        | PK                                    |
| `project_id`               | `uuid`        | FK → `projects`                       |
| `remote_session_issuer_id` | `uuid`        | FK                                    |
| `user_session_issuer_id`   | `uuid`        | FK; the issuer this client maps to;   |
| `client_id`                | `text`        |                                       |
| `client_secret_encrypted`  | `text`        | nullable for PKCE-only public clients |
| `client_id_issued_at`      | `timestamptz` |                                       |
| `client_secret_expires_at` | `timestamptz` | nullable for non-expiring secrets     |
| timestamps                 |               |                                       |

#### `user_session_consents`

Persistent consent record per (principal, `user_session_client`). Per-client, not per-issuer (see §2b "Consent record" / §3.4).

| Column                   | Type          | Notes                                                                                                   |
| ------------------------ | ------------- | ------------------------------------------------------------------------------------------------------- |
| `id`                     | `uuid`        | PK                                                                                                      |
| `principal_urn`          | `text`        | `user:<id>` \| `apikey:<uuid>` \| `anonymous:<mcp-session-id>`                                          |
| `user_session_client_id` | `uuid`        | FK; client implies its issuer                                                                           |
| `remote_set_hash`        | `text`        | SHA-256 of the sorted list of `remote_session_issuer_id`s on the client's owning issuer at consent time |
| `consented_at`           | `timestamptz` |                                                                                                         |
| timestamps               |               | unique index on `(principal_urn, user_session_client_id, remote_set_hash)`                              |

#### `user_sessions`

The issued user session. Created lazily at token exchange. Lookup at `/token` is by `refresh_token_hash`; bookkeeping ("what active sessions does this principal have at this issuer?") is a `(principal_urn, user_session_issuer_id)` query.

| Column                   | Type          | Notes                                                                                        |
| ------------------------ | ------------- | -------------------------------------------------------------------------------------------- |
| `id`                     | `uuid`        | PK                                                                                           |
| `user_session_issuer_id` | `uuid`        | FK                                                                                           |
| `principal_urn`          | `text`        | resolved principal — `user:<id>` \| `apikey:<uuid>` \| `anonymous:<mcp-session-id>`          |
| `jti`                    | `text`        | current access-token JTI; used by the revocation path                                        |
| `refresh_token_hash`     | `text`        | SHA-256 of the refresh token; lookup key at `/token`                                         |
| `refresh_expires_at`     | `timestamptz` | next refresh deadline; need not align with `expires_at`, but must be `<= expires_at`         |
| `expires_at`             | `timestamptz` | terminal session expiry; ceiling on `refresh_expires_at`; set from issuer `session_duration` |
| timestamps               |               | unique index on `refresh_token_hash`; index on `(principal_urn, user_session_issuer_id)`     |

#### `remote_sessions`

The remote-side OAuth session per `(principal, remote_session_client)`. Holds upstream access + refresh tokens with **independent** expiries. Created when a remote auth dance completes; refreshed silently on the access-expiry path.

| Column                     | Type          | Notes                                                       |
| -------------------------- | ------------- | ----------------------------------------------------------- |
| `id`                       | `uuid`        | PK                                                          |
| `principal_urn`            | `text`        | scoping (per §3.4)                                          |
| `user_session_issuer_id`   | `uuid`        | FK                                                          |
| `remote_session_client_id` | `uuid`        | FK; one client implies one issuer                           |
| `access_token_encrypted`   | `text`        |                                                             |
| `access_expires_at`        | `timestamptz` | independent of `refresh_expires_at`                         |
| `refresh_token_encrypted`  | `text`        | nullable                                                    |
| `refresh_expires_at`       | `timestamptz` | nullable                                                    |
| `scopes`                   | `text[]`      |                                                             |
| timestamps                 |               | unique index on `(principal_urn, remote_session_client_id)` |

#### Toolset link

`toolsets` gains:

| Column                   | Type   | Notes                                                                                                              |
| ------------------------ | ------ | ------------------------------------------------------------------------------------------------------------------ |
| `user_session_issuer_id` | `uuid` | FK → `user_session_issuers`; nullable; replaces both legacy `external_oauth_server_id` and `oauth_proxy_server_id` |

(`mcp_servers` mirrors this column whenever its runtime migration lands, per §3.5.)

### 4.2 Postgres — removed

| What                                           | Why                                                                                                                                                                      |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `oauth_proxy_servers`                          | Replaced by `user_session_issuers`.                                                                                                                                      |
| `oauth_proxy_providers`                        | Replaced by `remote_session_issuers` + `remote_session_clients`.                                                                                                         |
| `oauth_proxy_providers.secrets` JSONB          | Deprecated per goal #3; structured columns on `remote_session_clients` instead.                                                                                          |
| `oauth_proxy_providers.security_key_names`     | Deprecated per goal #3.                                                                                                                                                  |
| `oauth_proxy_providers.provider_type='custom'` | Deprecated per goal #3; behaviour collapses onto `passthrough` flag.                                                                                                     |
| `external_oauth_server_metadata`               | Use case preserved as `remote_session_issuer` with `passthrough=true`.                                                                                                   |
| `toolsets.external_oauth_server_id`            | Replaced by `toolsets.user_session_issuer_id`.                                                                                                                           |
| `toolsets.oauth_proxy_server_id`               | Replaced by `toolsets.user_session_issuer_id`.                                                                                                                           |
| `toolsets_oauth_exclusivity` CHECK             | No longer needed; only one column to set.                                                                                                                                |
| `oauth_proxy_client_info`                      | Empty in production — never written. The Postgres table is orphan structure; the live DCR registry is the Redis key `oauthClientInfo:*` (§4.4). Drop the table outright. |

Each removal becomes a ticket in `project.md`.

### 4.3 Redis — new types

Redis carries only short-TTL in-flight records. Durable session state (`user_sessions`, `remote_sessions`) lives in Postgres — see §4.1.

All Redis types implement `cache.CacheableObject[T]`; values are JSON-serialised by `cache.TypedCacheObject[T]`. Encrypted fields use `encryption.Client` before serialisation.

| Type                                 | Cache key                                       | Holds                                                                                                                                                                   | TTL          |
| ------------------------------------ | ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `AuthnChallengeState`                | `authnChallenge:{id}`                           | MCP Client OAuth context (`client_id`, `redirect_uri`, `code_challenge`, original `state`, scope), `user_session_issuer_id`, resolved principal URN (set after Phase 2) | ~10 min      |
| `UserSessionGrant`                   | `userSessionGrant:{userSessionIssuerID}:{code}` | `client_id`, redirect_uri, scope, state, PKCE challenge, principal URN                                                                                                  | ~10 min      |
| `RemoteSessionAuthState`             | `remoteSessionAuthState:{stateID}`              | principal URN, issuer id, client id, code verifier, redirect                                                                                                            | ~10 min      |
| `RemoteSessionPKCE`                  | `remoteSessionPKCE:{nonce}`                     | verifier                                                                                                                                                                | 10 min fixed |
| `RevokedToken` _(unchanged, reused)_ | `chat_session_revoked:{jti}`                    | jti, revoked_at                                                                                                                                                         | 24h          |

### 4.4 Redis — removed

| What                                           | Why                                                                                                                                                                                                                                                                                                                                                            |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `oauthGrant:{toolsetID}:{code}`                | Replaced by `userSessionGrant:*` keyed on `user_session_issuer_id`.                                                                                                                                                                                                                                                                                            |
| `oauthToken:{toolsetID}:{accessToken}`         | Eliminated. Access tokens are validated as JWTs (no Redis read on the validate path).                                                                                                                                                                                                                                                                          |
| `oauthRefreshToken:{toolsetID}:{refreshToken}` | Replaced by the Postgres `user_sessions` table (lookup by `refresh_token_hash`).                                                                                                                                                                                                                                                                               |
| `oauthClientInfo:{mcpURL}:{clientID}`          | This is the **actual** legacy DCR registry — `OauthProxyClientInfo` is written here, not to the Postgres `oauth_proxy_client_info` table (which is empty in production). Replaced by the Postgres `user_session_clients` table (§4.1). Existing live entries expire on their own with the secret TTL; cutover stops new writes and lets Redis drain naturally. |
| `upstreamPKCE:{nonce}`                         | Renamed `remoteSessionPKCE:*`.                                                                                                                                                                                                                                                                                                                                 |
| `externalOAuthState:{stateID}`                 | Renamed `remoteSessionAuthState:*`.                                                                                                                                                                                                                                                                                                                            |
| `Token.ExternalSecrets` (sub-field)            | The whole "tunnel upstream credentials through the AS token" pattern is gone. Remote credentials live in the Postgres `remote_sessions` table.                                                                                                                                                                                                                 |

### 4.5 JWT — unified `SessionClaims`

One claim shape for chat sessions and user sessions. Same signing key (`GRAM_JWT_SIGNING_KEY`), same algorithm (HS256), same revocation cache (`chat_session_revoked:{jti}`). Differs only in `sub` and `aud`.

The JWT carries **only the standard OIDC registered claims** — no Gram-specific extras. Anything else (org, project, etc.) is resolved from the session record in Redis.

```go
type SessionClaims struct {
    // OIDC-shaped registered claims
    Issuer    string   `json:"iss"`           // Gram issuer URL
    Subject   string   `json:"sub"`           // user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>
    Audience  []string `json:"aud"`           // toolset slug (user session) | embed origin (chat session)
    ExpiresAt int64    `json:"exp"`
    IssuedAt  int64    `json:"iat"`
    JTI       string   `json:"jti"`           // UUIDv4
}
```

Notes:

We take this opportunity to unify all of Gram Elements' authentication schemes with the MCP authentication scheme. There is no longer a distinct header for Elements — it uses the same JWT as the rest of `/mcp/*`.

### 4.6 SDL artifacts

The schemas above are the rationale; the canonical artifacts live in `/schemas/`:

- `/schemas/postgres.sql` — DDL for the new tables and the toolset alteration.
- `/schemas/redis.go` — Go-SDL definitions for each Redis type.
- `/schemas/jwt.go` — `SessionClaims` and the valid `sub` URN shapes.

---

## 5. Flows

> Backend errors and their relationship to OAuth are **out of scope** for this project. We may allow semantics for backends to reject and request new tokens, but we will consider this problem in a separate scope of work.

### 5.1 `mcp-handler` — state machine

[`diagrams/mcp-handler-states.mermaid`](diagrams/mcp-handler-states.mermaid)

The Token Managers return a generic `AuthnChallengeRequiredError`. `mcp/authn_challenge.go` is responsible for coercing system state into the OAuth dance required to resolve this authn challenge.

### 5.2 Authn Challenge - Chain Mode

**Initial State.** The toolset is configured with a single `user_session_issuer` whose `remote_set` contains one `remote_session_issuer` pointing at Linear:

| Field                    | Value                                      |
| ------------------------ | ------------------------------------------ |
| `issuer`                 | `https://login.linear.com`                 |
| `authorization_endpoint` | `https://login.linear.com/oauth/authorize` |
| `token_endpoint`         | `https://login.linear.com/oauth/token`     |
| `oidc`                   | `false`                                    |
| `passthrough`            | `false`                                    |

- Sequence: [`diagrams/unified-authn-challenge-chain.mermaid`](diagrams/unified-authn-challenge-chain.mermaid)
- State machine (shared with §5.3): [`diagrams/unified-authn-challenge-states.mermaid`](diagrams/unified-authn-challenge-states.mermaid)

### 5.3 Authn Challenge - Interactive Mode

Same Initial State as §5.2.

- Sequence: [`diagrams/unified-authn-challenge-interactive.mermaid`](diagrams/unified-authn-challenge-interactive.mermaid)
- State machine (shared with §5.2): [`diagrams/unified-authn-challenge-states.mermaid`](diagrams/unified-authn-challenge-states.mermaid)

---

## 6. Proposed Endpoints

This section catalogs every HTTP surface this RFC introduces or materially reshapes. It splits into two halves:

- **Management APIs** (§6.1, §6.2) — Goa-designed RPC under `/rpc/<service>.<method>`, consumed by the dashboard, CLI, and public SDK. See the `gram-management-api` skill for conventions.
- **OAuth endpoints** (§6.3, §6.4, §6.5) — the standards-shaped routes that participate in OAuth flows: discovery (§6.3), Gram-as-Authorization-Server (§6.4), and Gram-as-OAuth-Client (§6.5).

Path conventions in this section:

- `{slug}` resolves a `mcp_server` (or, transitionally, a toolset) by slug.
- Endpoints rooted at `/rpc/` are JSON-RPC-shaped management APIs.
- Endpoints rooted at `/mcp/` are part of the public MCP surface and follow the relevant OAuth or MCP specs.

### 6.1 User Session Management APIs

Surface: configure how Gram issues user sessions, and inspect/revoke what's been issued.

All methods are `POST` to `/rpc/<service>.<method>`. Project context is implicit from the caller's session. RBAC: every method gates on a `user_session_issuer` resource scope (see `gram-rbac`). Audit logging covers every mutation (see `gram-audit-logging`).

Pagination on `list` methods follows the standard Gram cursor convention: optional `cursor` (string) and `limit` (int, default `50`, max `100`); response carries a `next_cursor` (string, empty when exhausted).

#### `userSessionIssuers.create`

Create a `user_session_issuer`.

**Request**

| Field                  | Type     | Required | Notes                            |
| ---------------------- | -------- | -------- | -------------------------------- |
| `slug`                 | string   | required | unique per project               |
| `authn_challenge_mode` | enum     | required | `chain` \| `interactive`         |
| `session_duration`     | duration | required | ISO 8601 duration (e.g. `PT24H`) |

**Response**: full `UserSessionIssuer` record (see §4.1).

#### `userSessionIssuers.update`

Mutate fields on an existing issuer. All non-id fields are optional patches.

**Request**

| Field                  | Type     | Required | Notes                    |
| ---------------------- | -------- | -------- | ------------------------ |
| `id`                   | uuid     | required |                          |
| `slug`                 | string   | optional | rename                   |
| `authn_challenge_mode` | enum     | optional | `chain` \| `interactive` |
| `session_duration`     | duration | optional |                          |

**Response**: updated `UserSessionIssuer` record.

#### `userSessionIssuers.list`

List issuers in the caller's project.

**Request**

| Field    | Type   | Required | Notes                   |
| -------- | ------ | -------- | ----------------------- |
| `cursor` | string | optional | pagination cursor       |
| `limit`  | int    | optional | default `50`, max `100` |

**Response**: `{ items: UserSessionIssuer[], next_cursor: string }`.

#### `userSessionIssuers.get`

Detail by id or slug. Exactly one of `id` or `slug` must be supplied.

**Request**

| Field  | Type   | Required          | Notes |
| ------ | ------ | ----------------- | ----- |
| `id`   | uuid   | required (one of) |       |
| `slug` | string | required (one of) |       |

**Response**: `UserSessionIssuer` record.

#### `userSessionIssuers.delete`

Soft-delete an issuer. App-level cleanup cascades to dependent `user_sessions`, `user_session_consents`, and the `remote_session_clients` rows pointing at it.

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: empty.

#### `userSessions.list`

Operator visibility into issued user sessions. Read-only — sessions are written by `/mcp/{slug}/token` (§6.4).

**Request**

| Field                    | Type   | Required | Notes                   |
| ------------------------ | ------ | -------- | ----------------------- |
| `principal_urn`          | string | optional | exact-match filter      |
| `user_session_issuer_id` | uuid   | optional | filter                  |
| `cursor`                 | string | optional |                         |
| `limit`                  | int    | optional | default `50`, max `100` |

**Response**: `{ items: UserSession[], next_cursor: string }`. **`refresh_token_hash` is never returned over this surface.**

#### `userSessions.revoke`

Push the session's `jti` into the revocation cache (`chat_session_revoked:{jti}`, spike §4.5) and soft-delete the row.

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: empty.

#### `userSessionConsents.list`

List consent records for the caller's project.

**Request**

| Field                    | Type   | Required | Notes                                         |
| ------------------------ | ------ | -------- | --------------------------------------------- |
| `principal_urn`          | string | optional | filter                                        |
| `user_session_client_id` | uuid   | optional | filter                                        |
| `user_session_issuer_id` | uuid   | optional | filter (joins through `user_session_clients`) |
| `cursor`                 | string | optional |                                               |
| `limit`                  | int    | optional | default `50`, max `100`                       |

**Response**: `{ items: UserSessionConsent[], next_cursor: string }`.

#### `userSessionConsents.revoke`

Withdraw consent. Next `/mcp/{slug}/authorize` from any session matching `(principal_urn, user_session_client_id)` will re-prompt.

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: empty.

#### `userSessionClients.list`

Operator visibility into DCR'd MCP clients. Read-only — registrations are written by `/mcp/{slug}/register` (§6.4).

**Request**

| Field                    | Type   | Required | Notes                   |
| ------------------------ | ------ | -------- | ----------------------- |
| `user_session_issuer_id` | uuid   | optional | filter                  |
| `cursor`                 | string | optional |                         |
| `limit`                  | int    | optional | default `50`, max `100` |

**Response**: `{ items: UserSessionClient[], next_cursor: string }`. **`client_secret_hash` is never returned.**

#### `userSessionClients.get`

Detail by id.

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: `UserSessionClient` record (no `client_secret_hash`).

#### `userSessionClients.revoke`

Soft-delete a registration. Future tokens minted for this `client_id` are rejected; existing live `user_sessions` keep working until they hit `expires_at`.

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: empty.

### 6.2 Remote Session Management APIs

Surface: configure upstream issuers and the credentials Gram presents to them, and inspect/revoke remote sessions Gram is holding on a principal's behalf. Same conventions as §6.1 (POST to `/rpc/<service>.<method>`, project context implicit, RBAC + audit, standard pagination on `list`).

#### `remoteSessionIssuers.discover`

One-shot helper. Hits the upstream `<issuer>/.well-known/oauth-authorization-server` (RFC 8414) and returns a draft suitable for passing to `create`. No persistence.

**Request**

| Field    | Type   | Required | Notes                                                    |
| -------- | ------ | -------- | -------------------------------------------------------- |
| `issuer` | string | required | issuer URL to discover (e.g. `https://login.linear.com`) |

**Response**: a `RemoteSessionIssuerDraft` — same field set as a `RemoteSessionIssuer` (§4.1) minus `id`/`project_id`/timestamps, plus a `discovery_warnings: string[]` field describing any RFC 8414 deviations.

#### `remoteSessionIssuers.create`

Create a `remote_session_issuer`. Typically the body is a draft from `discover` with a project-unique `slug` set.

**Request**

| Field                                   | Type     | Required | Notes                          |
| --------------------------------------- | -------- | -------- | ------------------------------ |
| `slug`                                  | string   | required | unique per project             |
| `issuer`                                | string   | required | matches `iss` claim            |
| `authorization_endpoint`                | string   | optional |                                |
| `token_endpoint`                        | string   | optional |                                |
| `registration_endpoint`                 | string   | optional | absent for issuers without DCR |
| `jwks_uri`                              | string   | optional |                                |
| `scopes_supported`                      | string[] | optional |                                |
| `grant_types_supported`                 | string[] | optional |                                |
| `response_types_supported`              | string[] | optional |                                |
| `token_endpoint_auth_methods_supported` | string[] | optional |                                |
| `oidc`                                  | bool     | optional | default `false`                |
| `passthrough`                           | bool     | optional | default `false`                |

**Response**: full `RemoteSessionIssuer` record.

#### `remoteSessionIssuers.update`

Mutate fields. All non-id fields are optional patches.

**Request**

| Field                     | Type       | Required | Notes         |
| ------------------------- | ---------- | -------- | ------------- |
| `id`                      | uuid       | required |               |
| _any field from `create`_ | _as above_ | optional | partial patch |

**Response**: updated `RemoteSessionIssuer` record.

#### `remoteSessionIssuers.list`

**Request**

| Field    | Type   | Required | Notes                   |
| -------- | ------ | -------- | ----------------------- |
| `cursor` | string | optional |                         |
| `limit`  | int    | optional | default `50`, max `100` |

**Response**: `{ items: RemoteSessionIssuer[], next_cursor: string }`.

#### `remoteSessionIssuers.get`

Detail by id or slug. Exactly one of `id` or `slug` must be supplied.

**Request**

| Field  | Type   | Required          | Notes |
| ------ | ------ | ----------------- | ----- |
| `id`   | uuid   | required (one of) |       |
| `slug` | string | required (one of) |       |

**Response**: `RemoteSessionIssuer` record.

#### `remoteSessionIssuers.delete`

Soft-delete an issuer. Blocked if any `remote_session_clients` still reference it.

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: empty.

#### `remoteSessionClients.create`

Register Gram-side credentials against an issuer. Two paths:

- **Manual credentials**: caller supplies `client_id` and (optionally) `client_secret`. Gram encrypts and stores.
- **Auto-DCR**: caller omits credentials; Gram fires the `<issuer>.registration_endpoint` outbound call (§6.5) and persists the result.

**Request**

| Field                      | Type   | Required          | Notes                                                    |
| -------------------------- | ------ | ----------------- | -------------------------------------------------------- |
| `remote_session_issuer_id` | uuid   | required          |                                                          |
| `user_session_issuer_id`   | uuid   | required          | which Gram AS this client is paired with                 |
| `client_id`                | string | required (one of) | when manual                                              |
| `client_secret`            | string | optional          | manual only; Gram encrypts                               |
| `auto_register`            | bool   | required (one of) | `true` triggers DCR; `client_id`/`client_secret` ignored |

**Response**: `RemoteSessionClient` record (no `client_secret_encrypted` returned).

#### `remoteSessionClients.update`

Rotate the secret or change the `user_session_issuer_id` linkage.

**Request**

| Field                    | Type   | Required | Notes                    |
| ------------------------ | ------ | -------- | ------------------------ |
| `id`                     | uuid   | required |                          |
| `client_secret`          | string | optional | rotate; Gram re-encrypts |
| `user_session_issuer_id` | uuid   | optional | re-pair                  |

**Response**: updated `RemoteSessionClient` record.

#### `remoteSessionClients.list`

**Request**

| Field                      | Type   | Required | Notes                   |
| -------------------------- | ------ | -------- | ----------------------- |
| `remote_session_issuer_id` | uuid   | optional | filter                  |
| `user_session_issuer_id`   | uuid   | optional | filter                  |
| `cursor`                   | string | optional |                         |
| `limit`                    | int    | optional | default `50`, max `100` |

**Response**: `{ items: RemoteSessionClient[], next_cursor: string }`.

#### `remoteSessionClients.get`

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: `RemoteSessionClient` record.

#### `remoteSessionClients.delete`

Soft-delete. Cascades to `remote_sessions` rows pointing at this client (existing principals are forced to re-authenticate).

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: empty.

#### `remoteSessions.list`

Inspect issued remote sessions. Read-only — sessions are written by `/mcp/{slug}/remote_login_callback` (§6.5) and the silent-refresh path.

**Request**

| Field                      | Type   | Required | Notes                   |
| -------------------------- | ------ | -------- | ----------------------- |
| `principal_urn`            | string | optional | filter                  |
| `remote_session_client_id` | uuid   | optional | filter                  |
| `cursor`                   | string | optional |                         |
| `limit`                    | int    | optional | default `50`, max `100` |

**Response**: `{ items: RemoteSession[], next_cursor: string }`. **`access_token_encrypted` and `refresh_token_encrypted` are never returned** — only metadata (`access_expires_at`, `refresh_expires_at`, `scopes`).

#### `remoteSessions.revoke`

Drop a `remote_session` row. Next `/mcp` call by that principal triggers a fresh authn challenge.

**Request**

| Field | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `id`  | uuid | required |       |

**Response**: empty.

### 6.3 MCP OAuth Endpoints

Discovery surface a compliant MCP client hits before it knows how to authenticate. Per the MCP authorization spec, both well-known docs are scoped to the `mcp_server`'s `/mcp/{slug}` path. Both are derived from the `user_session_issuer` configuration; they are not stored as separate records.

#### `GET /mcp/{slug}/.well-known/oauth-protected-resource`

RFC 9728 OAuth 2.0 Protected Resource Metadata.

**Path Params**

| Field  | Notes                                                   |
| ------ | ------------------------------------------------------- |
| `slug` | resolves a `mcp_server` (or, transitionally, a toolset) |

**Response (200, JSON)** — key fields per RFC 9728:

- `resource` — canonical URL of this MCP server
- `authorization_servers` — array of issuer URLs (the `user_session_issuer`'s metadata URL)
- `scopes_supported` — derived from the issuer's policy
- `bearer_methods_supported` — typically `["header"]`

#### `GET /mcp/{slug}/.well-known/oauth-authorization-server`

RFC 8414 Authorization Server Metadata for Gram-as-AS.

**Path Params**

| Field  | Notes                                                   |
| ------ | ------------------------------------------------------- |
| `slug` | resolves a `mcp_server` (or, transitionally, a toolset) |

**Response (200, JSON)** — key fields per RFC 8414:

- `issuer` — Gram issuer URL
- `authorization_endpoint` — `<gram>/mcp/{slug}/authorize`
- `token_endpoint` — `<gram>/mcp/{slug}/token`
- `registration_endpoint` — `<gram>/mcp/{slug}/register`
- `revocation_endpoint` — `<gram>/mcp/{slug}/revoke`
- `scopes_supported` — issuer policy
- `response_types_supported` — `["code"]`
- `grant_types_supported` — `["authorization_code", "refresh_token"]`
- `token_endpoint_auth_methods_supported` — `["client_secret_basic", "none"]` (see §8 open question)
- `code_challenge_methods_supported` — `["S256"]`

### 6.4 User Session OAuth Endpoints

Gram-as-Authorization-Server. Implements the AS half of OAuth 2.1 + DCR for MCP clients. Lives in `usersessions/`. All paths are rooted at `/mcp/{slug}` where `slug` resolves a `mcp_server` (or, transitionally, a toolset).

#### `POST /mcp/{slug}/register`

Dynamic Client Registration per RFC 7591.

**Body (JSON)**

| Field                        | Type     | Required | Notes                                               |
| ---------------------------- | -------- | -------- | --------------------------------------------------- |
| `client_name`                | string   | required |                                                     |
| `redirect_uris`              | string[] | required |                                                     |
| `grant_types`                | string[] | optional | must intersect issuer-supported; rejected otherwise |
| `response_types`             | string[] | optional | must intersect issuer-supported                     |
| `token_endpoint_auth_method` | string   | optional | must be issuer-supported                            |
| `scope`                      | string   | optional | space-separated; must intersect `scopes_supported`  |

**Response (201, JSON)** per RFC 7591:

- `client_id` — generated
- `client_secret` — generated; **returned exactly once**, stored as hash
- `client_id_issued_at`
- `client_secret_expires_at` — `0` if non-expiring
- echo of `client_name`, `redirect_uris`

**Side effects**: writes to `user_session_clients` (§4.1).

#### `GET /mcp/{slug}/authorize`

OAuth 2.1 authorization endpoint (RFC 6749 §4.1) and Phase 1 entry of the unified authn challenge flow.

**Query Params**

| Field                   | Required    | Notes                                                   |
| ----------------------- | ----------- | ------------------------------------------------------- |
| `client_id`             | required    |                                                         |
| `redirect_uri`          | required    | must match a `user_session_clients.redirect_uris` entry |
| `response_type`         | required    | must be `code`                                          |
| `state`                 | recommended | echoed back on redirect                                 |
| `scope`                 | optional    |                                                         |
| `code_challenge`        | required    | PKCE per RFC 7636                                       |
| `code_challenge_method` | required    | `S256`                                                  |

**Response**: 302 redirect — to Speakeasy IDP (Phase 2), to `/mcp/{slug}/connect` (Phase 3/4), or directly to `redirect_uri?code=…&state=…` (when all authn challenges already satisfied for this principal/client).

**Side effects**: writes `AuthnChallengeState` to Redis (§4.3).

#### `GET /mcp/{slug}/client_login_callback`

Phase 2 callback after the user authenticates with the Gram identity provider (Speakeasy IDP today; see §8).

**Query Params**

| Field   | Required | Notes                              |
| ------- | -------- | ---------------------------------- |
| `code`  | required | IDP authorization code             |
| `state` | required | refers to `AuthnChallengeState.id` |

**Response**: 302 redirect — to the next required authn challenge (a remote authorize endpoint or `/mcp/{slug}/connect`).

**Side effects**: exchanges IDP code for ID token; verifies org membership; stamps resolved principal onto `AuthnChallengeState`.

#### `GET, POST /mcp/{slug}/connect`

Phase 3/4 UI. The connect page.

**Query Params (GET)**

| Field   | Required | Notes                    |
| ------- | -------- | ------------------------ |
| `state` | required | `AuthnChallengeState.id` |

**Form Params (POST)**

| Field                   | Required | Notes                                                                                                       |
| ----------------------- | -------- | ----------------------------------------------------------------------------------------------------------- |
| `state`                 | required |                                                                                                             |
| `remote_session_client` | optional | when supplied, server starts that remote's auth flow. When absent, server treats submission as Give Access. |

**Response**:

- `GET` → 200, HTML page (per-remote Connect buttons + Give Access button)
- `POST` with `remote_session_client` → 302 to that remote's `authorization_endpoint`
- `POST` without → 302 to the MCP client's `redirect_uri?code=…&state=<original>`

**Side effects**: on Give Access, writes `user_session_consents` row when missing or when `remote_set_hash` mismatches. On Give Access, mints `UserSessionGrant` (Redis, §4.3).

#### `POST /mcp/{slug}/token`

OAuth 2.1 token endpoint (RFC 6749 §4.1.3 / §6).

**Form Params**

| Field           | Required                        | Notes                                    |
| --------------- | ------------------------------- | ---------------------------------------- |
| `grant_type`    | required                        | `authorization_code` \| `refresh_token`  |
| `code`          | required (auth code grant)      |                                          |
| `redirect_uri`  | required (auth code grant)      | must match `/authorize`                  |
| `code_verifier` | required (auth code grant)      | PKCE                                     |
| `refresh_token` | required (refresh grant)        |                                          |
| `client_id`     | required                        |                                          |
| `client_secret` | required (confidential clients) | via HTTP Basic _or_ form (issuer policy) |

**Response (200, JSON)**:

- `access_token` — JWT (`SessionClaims`, §4.5)
- `token_type` — `Bearer`
- `expires_in` — seconds
- `refresh_token`
- `scope`

**Side effects**: writes `user_sessions` row keyed on `refresh_token_hash`; the previous row for this `(principal_urn, user_session_client_id)` is soft-deleted on rotation.

#### `POST /mcp/{slug}/revoke`

RFC 7009 token revocation.

**Form Params**

| Field             | Required | Notes                             |
| ----------------- | -------- | --------------------------------- |
| `token`           | required |                                   |
| `token_type_hint` | optional | `access_token` \| `refresh_token` |

**Response (200)**: empty body.

**Side effects**: pushes `jti` into `chat_session_revoked:{jti}` (24h TTL); soft-deletes the `user_sessions` row.

### 6.5 Remote Session OAuth Endpoints

Gram-as-OAuth-Client. One inbound callback plus four outbound contracts. Lives in `remotesessions/`. Passthrough mode short-circuits this whole surface (per spike §2b): the MCP client transacts with the remote issuer directly rather than redirecting through Gram.

#### `GET /mcp/{slug}/remote_login_callback` (inbound)

Callback after the user authenticates with a `remote_session_issuer`.

**Query Params**

| Field   | Required | Notes                                       |
| ------- | -------- | ------------------------------------------- |
| `code`  | required | upstream authorization code                 |
| `state` | required | refers to `RemoteSessionAuthState.state_id` |

**Response**: 302 redirect → `/mcp/{slug}/connect?state=<authn-challenge-state-id>`.

**Side effects**: looks up `RemoteSessionAuthState` and `RemoteSessionPKCE` (Redis, §4.3); exchanges the code at the upstream `token_endpoint`; writes/updates the `remote_sessions` row.

#### `GET <remote_session_issuer>/.well-known/oauth-authorization-server` (outbound)

RFC 8414 discovery.

**Trigger**: operator runs `remoteSessionIssuers.discover` (§6.2).

**Response handling**: parsed per RFC 8414. Returned to the operator for confirmation before `remoteSessionIssuers.create`. No persistence on the discovery hit itself.

#### `POST <remote_session_issuer>.registration_endpoint` (outbound)

RFC 7591 DCR — auto-registers a `remote_session_client`.

**Trigger**: `remoteSessionClients.create` with `auto_register=true`, against an issuer that has a non-null `registration_endpoint`.

**Body**: per RFC 7591 (`client_name`, `redirect_uris`, `grant_types`, `response_types`, `token_endpoint_auth_method`).

**Side effects**: writes `remote_session_clients` row with returned `client_id` and (encrypted) `client_secret`.

#### `<remote_session_issuer>.authorization_endpoint` (outbound, browser-mediated 302)

The 302 from §5.2 / §5.3 Phase 3.

**Trigger**: when a remote session is missing or expired during an authn challenge flow.

**Query Params (sent to upstream)**:

| Field                   | Notes                                                |
| ----------------------- | ---------------------------------------------------- |
| `client_id`             | from `remote_session_client.client_id`               |
| `redirect_uri`          | Gram's `/mcp/{slug}/remote_login_callback`           |
| `response_type`         | `code`                                               |
| `state`                 | `RemoteSessionAuthState.state_id`                    |
| `code_challenge`        | PKCE                                                 |
| `code_challenge_method` | `S256`                                               |
| `scope`                 | per the `remote_session_issuer`'s `scopes_supported` |

**Side effects**: writes `RemoteSessionAuthState` and `RemoteSessionPKCE` (Redis) before the redirect.

#### `POST <remote_session_issuer>.token_endpoint` (outbound)

Code-for-token exchange and silent refresh.

**Triggers**:

- Code exchange — fired by `/mcp/{slug}/remote_login_callback` (above).
- Silent refresh — fired by `remotesessions/` when `access_expires_at` is near and a `refresh_token_encrypted` is present.

**Form Params**:

- For code exchange: `grant_type=authorization_code`, `code`, `redirect_uri`, `client_id`, `code_verifier`, plus `client_secret` (decrypted from `remote_session_clients`) when applicable.
- For refresh: `grant_type=refresh_token`, `refresh_token`, `client_id`, `client_secret` (when applicable).

**Side effects**: writes/updates `remote_sessions` with returned `access_token` (encrypted), `refresh_token` (encrypted), `access_expires_at`, `refresh_expires_at`, `scopes`.

---

## 7. Out of scope

Items called out inline elsewhere in this RFC, consolidated here for convenience:

- **Playground OAuth.** The `user_oauth_tokens` table and the playground UX flows are entirely unrelated to this work and stay untouched.
- **`Gram-Chat-Session` header migration.** This RFC unifies chat sessions onto `Authorization: Bearer` JWTs at the schema level (§4.5). Sunset of the legacy header is its own follow-up stack.
- **Backend errors and OAuth.** Backends may want semantics for rejecting requests and forcing fresh tokens (e.g. on a 401 from upstream). Out of scope here — see §5 preamble.
- **Just-in-time authn challenge mode.** Issuing remote-session authn challenges only for the specific tokens a request actually uses, rather than the full set the issuer requires. See §2b.
- **Sharing remote sessions across principals.** Multiple principals using the same upstream credentials (e.g. a customer-provided shared OAuth client against a shared Notion MCP). Carved out as Milestone #10.
- **Permission model for `remote_session_clients`.** Customer-provided client IDs and secrets need access-control rules; out of initial scope (§2b).
- **Cross-project shared `remote_session_issuer`s.** Schema currently scopes them per project; long-term we want shared configurations across projects (TODO in `schemas/postgres.sql`).
- **Fine-grained scope-level consent.** Consent is bound to the full set of `remote_session_issuer`s on the user-session client's owning issuer, not to individual remotes or per-scope (§3.4).
- **`mcp_servers` runtime migration.** The `mcp_servers.user_session_issuer_id` column lands when the parallel `mcp_endpoints` work is ready (§3.5). This RFC attaches at whichever level is current at implementation time.
- **Tampering with Gram management API sessions.** The management API's auth model is unchanged.
- **DCR rate limiting / abuse hardening.** `/register` is a public endpoint and an obvious target. Out of scope here; see §8.
- **MCP server lifecycle (onboarding, OAuth wizard, dashboard-driven DCR).** Code shipped on `main` since this branch was rebased — notably the `POST /oauth/proxy-register` handler in `server/internal/oauth/impl.go` (`handleProxyRegister`) and the OAuth Proxy wizard's auto-configure path — is part of the **MCP server onboarding** process. Onboarding is **not** in scope for this RFC. This work is bounded to the **session issuer lifecycle** (issuing, refreshing, revoking user/remote sessions) and the **MCP request lifecycle** (`/mcp/{slug}` runtime). Future RFCs may rationalize how onboarding-time DCR (which targets dashboard users registering Gram-as-Client against an upstream) lines up with the runtime-time DCR introduced here (§6.5: Gram firing `<remote_session_issuer>.registration_endpoint` on behalf of a `remote_session_client`), but that reconciliation is not this project.

---

## 8. Open questions

Items that surfaced during design that we haven't decided. Each is small enough to settle inline during implementation, but worth tracking before agents go heads-down on milestones.

- **Should `user_session_issuer` force authentication, or always defer to `mcp_servers`?** §4.1 leans toward defer-to-`mcp_servers`. We'll attempt that and revise if it falls over.
- **Will `usersessions/` need to read `remotesessions/` state?** Passthrough mode resolves principal identity from a bearer the MCP client minted at the upstream — i.e. `usersessions` may need to inspect remote-session state to complete identity resolution. If so, the dependency direction between the two packages stops being clean. We'll keep the dependency as narrow as the implementation actually requires and revisit if it starts carrying real weight.
- **`jti` uniqueness on `user_sessions`.** Currently no unique index. A duplicate JTI would silently work. Probably worth adding a unique index — low cost, defends against subtle bugs in token issuance.
- **Final list of `token_endpoint_auth_methods_supported`.** §6.3 advertises `client_secret_basic` + `none`. Whether to add `client_secret_post` is unsettled. Drop `none` for confidential clients only?
- **DCR rate limiting / abuse prevention.** `/mcp/{slug}/register` is public. An adversary could create thousands of `user_session_clients`. Per-IP and per-`mcp_server` rate limits are an obvious mitigation; whether to require a registration access token (RFC 7591 §3) is a real question.
- **Operator-level org-wide client visibility.** §6.1 `userSessionClients.list` filters by `user_session_issuer_id`. Should there be an org-wide cross-issuer view for security ops?
- **Should `userSessionClients.update` exist?** Currently no — DCR is the only writer. Operators may want to fix `redirect_uris` for a misconfigured client without forcing re-registration. Maybe yes for that one field only.
- **Speakeasy IDP dependency.** §5.2 / §5.3 use Speakeasy as the user-authentication provider. We may want to register Gram as a true OIDC RP at a more appropriate IDP (or run our own). Flagged inline in `diagrams/unified-authn-challenge-chain.mermaid`.
- **`remoteSessionClients.create` two-path API.** §6.2 has a single endpoint that branches on `auto_register`. Cleaner to split into `remoteSessionClients.create` (manual) and `remoteSessionClients.register` (auto-DCR)?
