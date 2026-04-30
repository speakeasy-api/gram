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

### 2b. Gram-specific terms

The following terms are introduced or redefined by this RFC. When the codebase still has legacy structures with overlapping names, the legacy term is called out explicitly. **Reviewers should take special care to comment and align on these terms.**

#### Client session

The session Gram maintains with an MCP client. A client session has exactly one principal (`user`, `apikey`, or `anonymous`), is bound to exactly one toolset, and is materialised as:

- A signed access token
- A Redis-backed refresh document, keyed `(session_id, client_session_issuer_id)`

This RFC also deprecates the legacy `Gram-Chat-Session` header. Chat-session JWTs become `Authorization`-header-delivered tokens that share the same JWT schema and signing key as client sessions; the two flows unify under one claim shape and one revocation path, differing only in `sub` and `aud`.

#### Client session issuer (`client_session_issuer`)

The Gram-side authorization-server configuration that issues client sessions for a toolset. Replaces today's `oauth_proxy_servers` row. A toolset that wants to gate MCP traffic with a Gram-issued session points at a `client_session_issuer`. A `client_session_issuer` may reference zero or more `remote_oauth_issuer`s — i.e. there can be multiple remote OAuth challenges to satisfy on the way to issuing a client session. (See _chain_ vs _interactive_ below for how those challenges are presented.)

#### Remote OAuth issuer (`remote_oauth_issuer`)

An upstream Authorization Server's identity record. Holds AS metadata (RFC 8414 fields), the issuer URL, and a required `oidc bool` flag (defaulted `false`) that may unlock OIDC-aware behaviour when `true`. Issuer rows can be auto-discovered (e.g. by hitting an upstream `/.well-known/oauth-authorization-server`) and are managed independently of the credentials Gram presents to the issuer.

Conceptually this is closer to today's `oauth_proxy_provider` than to `external_oauth_server_metadata`. The behavioural difference between the two legacy modes collapses onto a single `passthrough` flag on the issuer.

#### Remote OAuth client (`remote_oauth_client`)

Credentials Gram uses when acting as a client of a `remote_oauth_issuer`. One issuer can have many clients in the schema; in initial scope we use 1:1, but the structure leaves room for, e.g., multiple Notion-app credentials against a single shared Notion MCP. This serves as a jump table between `oauth_proxy_providers` and `client_session_issuers` — i.e. each `remote_oauth_client` should have its own `client_session_issuer` associated. We would likely be better served to have a single `remote_oauth_client` for each provider that can be shared across all of Gram, but we leave the definition of a permission model for client IDs and secrets (which can be customer-provided) out of scope here.

#### Passthrough mode (on a `remote_oauth_issuer`)

Mode where the bearer the MCP client sent us is forwarded to the upstream as-is, rather than Gram exchanging for an upstream token of its own. We still conform to the abstractions — if storing a remote session document is what homogeneity requires, we store one. Notably, the MCP client will register a client directly with the remote Authorization Server rather than with Gram. The access token will be delivered directly to that authorization server as it is received by the client.

This is the **same concept** as Milestone #2's _passthrough authentication_. The two names are aliases.

#### Chain vs Interactive (modes on a `client_session_issuer`)

Both modes describe how multi-remote OAuth challenges are presented after Gram issues a client session.

- **Chain.** From the Gram callback, redirect through each subsequent remote challenge in turn, build the entire session, then redirect back to the MCP client callback for the final token exchange. There is no intermediate UI listing the remotes. Consent must still be prompted _somewhere_ in the request stream — chain mode does not skip consent, it just doesn't render a "click each server" screen.
- **Interactive.** Gram issues the client session up front, then renders a UX where the user clicks each remote OAuth server to authenticate. This is the same screen that Milestone #8's URL-mode elicitation points at when refreshing stale remote credentials.
- **Just-In-Time** (out of scope). We only offer the challenge for tokens that the client is trying to use for _this_ given request. Similar behavior to _chain_ but more aggressive.

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
| Chain / Interactive challenge orchestration           | `mcp/` (orchestrates), `clientsessions/` + `remotesessions/` (execute primitives) |
| MCP auth challenge translation                        | `mcp/challenge.go`                                                                |
| Tool-call credential injection                        | Resolved by `mcp/`; presented to the backend as opaque inputs                     |

### 3.3 The MCP request — orchestration

`server/internal/mcp/` orchestrates each request in four steps:

1. **Resolve the credential to an identity.** Call `clientsessions` to validate whatever the client presented (Gram-issued JWT, or nothing — in which case the supplied `mcp_session_id` becomes the id of an anonymous session). Returns a principal.
2. **Authorize server access.** Check whether that principal is permitted to use this toolset.
3. **Request credentials from `remotesessions` for this identity.** Materialise (or refresh) every required remote session keyed on `(session_id, client_session_issuer_id)`. If anything is missing or stale, fire the appropriate challenge (chain / interactive / passthrough).
4. **Pass credentials to the MCP backend.** The toolset receives an opaque credential bundle. `doHTTP` consumes it without any OAuth knowledge — satisfying goal #7.

### 3.4 Where consent fires

Consent enforcement lives entirely in `clientsessions` at the `/mcp/connect` endpoint. The check is: "has this user previously consented for this `client_session_issuer` to access **all** of its `remote_session_tokens`?" Only a matching consent record short-circuits the prompt. Re-prompting is the default whenever the set of `remote_oauth_issuer`s on the issuer changes (i.e. consent is bound to the _full set_, not to individual remotes).

In both chain and interactive modes the consent prompt is the `/mcp/connect` page — interactive mode renders the per-remote Connect buttons alongside the grant-access button; chain mode lands on the page with all remotes already ✓ and only the grant-access button visible.

### 3.5 Relationship to the MCP server / backend split

Everything in this RFC — `client_session_issuer`, `remote_oauth_issuer`, `remote_oauth_client`, consent records, every Redis session document — attaches at the **`mcp_servers`** level (Gram's MCP-server configuration), not on the backend. The MCP backend (a `toolsets` row or a `remote_mcp_servers` row) consumes the credential bundle Gram has assembled — and that bundle absolutely includes upstream OAuth access tokens (e.g., a Linear bearer the backend forwards onto outbound calls). What the backend does **not** do is run the OAuth dance: collecting tokens, refreshing them, prompting for consent. That work stays MCP-server-level.

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
| `challenge_mode` | `text` | `chain` \| `interactive`                            |
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

#### `client_sessions`

The issued client session. Created lazily at token exchange. Lookup at `/token` is by `refresh_token_hash`; bookkeeping ("what active sessions does this principal have at this issuer?") is a `(principal_urn, client_session_issuer_id)` query.

| Column                     | Type          | Notes                                                                                      |
| -------------------------- | ------------- | ------------------------------------------------------------------------------------------ |
| `id`                       | `uuid`        | PK                                                                                         |
| `client_session_issuer_id` | `uuid`        | FK                                                                                         |
| `principal_urn`            | `text`        | resolved principal — `user:<id>` \| `apikey:<uuid>` \| `anonymous:<mcp-session-id>`        |
| `jti`                      | `text`        | current access-token JTI; used by the revocation path                                      |
| `refresh_token_hash`       | `text`        | SHA-256 of the refresh token; lookup key at `/token`                                       |
| `refresh_expires_at`       | `timestamptz` |                                                                                            |
| timestamps                 |               | unique index on `refresh_token_hash`; index on `(principal_urn, client_session_issuer_id)` |

#### `remote_sessions`

The remote-side OAuth session per `(principal, remote_oauth_client)`. Holds upstream access + refresh tokens with **independent** expiries. Created when a remote auth dance completes; refreshed silently on the access-expiry path.

| Column                     | Type          | Notes                                                     |
| -------------------------- | ------------- | --------------------------------------------------------- |
| `id`                       | `uuid`        | PK                                                        |
| `principal_urn`            | `text`        | scoping (per §3.4)                                        |
| `client_session_issuer_id` | `uuid`        | FK                                                        |
| `remote_oauth_client_id`   | `uuid`        | FK; one client implies one issuer                         |
| `access_token_encrypted`   | `text`        |                                                           |
| `access_expires_at`        | `timestamptz` | independent of `refresh_expires_at`                       |
| `refresh_token_encrypted`  | `text`        | nullable                                                  |
| `refresh_expires_at`       | `timestamptz` | nullable                                                  |
| `scopes`                   | `text[]`      |                                                           |
| timestamps                 |               | unique index on `(principal_urn, remote_oauth_client_id)` |

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

Redis carries only short-TTL in-flight records. Durable session state (`client_sessions`, `remote_sessions`) lives in Postgres — see §4.1.

All Redis types implement `cache.CacheableObject[T]`; values are JSON-serialised by `cache.TypedCacheObject[T]`. Encrypted fields use `encryption.Client` before serialisation.

| Type                                 | Cache key                                           | Holds                                                                                                                                                                     | TTL          |
| ------------------------------------ | --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `ChallengeState`                     | `challengeState:{id}`                               | MCP Client OAuth context (`client_id`, `redirect_uri`, `code_challenge`, original `state`, scope), `client_session_issuer_id`, resolved principal URN (set after Phase 2) | ~10 min      |
| `ClientSessionGrant`                 | `clientSessionGrant:{clientSessionIssuerID}:{code}` | `client_id`, redirect_uri, scope, state, PKCE challenge, principal URN                                                                                                    | ~10 min      |
| `RemoteSessionAuthState`             | `remoteSessionAuthState:{stateID}`                  | principal URN, issuer id, client id, code verifier, redirect                                                                                                              | ~10 min      |
| `RemoteSessionPKCE`                  | `remoteSessionPKCE:{nonce}`                         | verifier                                                                                                                                                                  | 10 min fixed |
| `RevokedToken` _(unchanged, reused)_ | `chat_session_revoked:{jti}`                        | jti, revoked_at                                                                                                                                                           | 24h          |

### 4.4 Redis — removed

| What                                           | Why                                                                                                                                            |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `oauthGrant:{toolsetID}:{code}`                | Replaced by `clientSessionGrant:*` keyed on `client_session_issuer_id`.                                                                        |
| `oauthToken:{toolsetID}:{accessToken}`         | Eliminated. Access tokens are validated as JWTs (no Redis read on the validate path).                                                          |
| `oauthRefreshToken:{toolsetID}:{refreshToken}` | Replaced by the Postgres `client_sessions` table (lookup by `refresh_token_hash`).                                                             |
| `oauthClientInfo:{mcpURL}:{clientID}`          | TBD — pairs with the `oauth_proxy_client_info` table decision.                                                                                 |
| `upstreamPKCE:{nonce}`                         | Renamed `remoteSessionPKCE:*`.                                                                                                                 |
| `externalOAuthState:{stateID}`                 | Renamed `remoteSessionAuthState:*`.                                                                                                            |
| `Token.ExternalSecrets` (sub-field)            | The whole "tunnel upstream credentials through the AS token" pattern is gone. Remote credentials live in the Postgres `remote_sessions` table. |

### 4.5 JWT — unified `SessionClaims`

One claim shape for chat sessions and client sessions. Same signing key (`GRAM_JWT_SIGNING_KEY`), same algorithm (HS256), same revocation cache (`chat_session_revoked:{jti}`). Differs only in `sub` and `aud`.

The JWT carries **only the standard OIDC registered claims** — no Gram-specific extras. Anything else (org, project, etc.) is resolved from the session record in Redis.

```go
type SessionClaims struct {
    // OIDC-shaped registered claims
    Issuer    string   `json:"iss"`           // Gram issuer URL
    Subject   string   `json:"sub"`           // user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>
    Audience  []string `json:"aud"`           // toolset slug (client session) | embed origin (chat session)
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

The Token Managers return a generic `ChallengeRequiredError`. `mcp/challenge.go` is responsible for coercing system state into the OAuth dance required to resolve this challenge.

### 5.2 Auth Challenge - Chain Mode

**Initial State.** The toolset is configured with a single `client_session_issuer` whose `remote_set` contains one `remote_oauth_issuer` pointing at Linear:

| Field                    | Value                                      |
| ------------------------ | ------------------------------------------ |
| `issuer`                 | `https://login.linear.com`                 |
| `authorization_endpoint` | `https://login.linear.com/oauth/authorize` |
| `token_endpoint`         | `https://login.linear.com/oauth/token`     |
| `oidc`                   | `false`                                    |
| `passthrough`            | `false`                                    |

- Sequence: [`diagrams/unified-challenge-chain.mermaid`](diagrams/unified-challenge-chain.mermaid)
- State machine (shared with §5.3): [`diagrams/unified-challenge-states.mermaid`](diagrams/unified-challenge-states.mermaid)

### 5.3 Auth Challenge - Interactive Mode

Same Initial State as §5.2.

- Sequence: [`diagrams/unified-challenge-interactive.mermaid`](diagrams/unified-challenge-interactive.mermaid)
- State machine (shared with §5.2): [`diagrams/unified-challenge-states.mermaid`](diagrams/unified-challenge-states.mermaid)

---

_[Sections 6–8 to be drafted next pass — Management API surface, Out of scope, Open questions.]_
