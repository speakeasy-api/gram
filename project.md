# Project — Remote OAuth Clients for Private Repos

We currently have a product need to secure OAuth servers with a Gram login, but we are presented with a problem: securing servers with Gram login uses the same method as securing credentials for upstream OAuth providers with some special behavior applied.

In order to remove this product constraint, we will introduce a new concept of **Remote Sessions** that a Gram user can own. These **Remote Sessions** can then be accessed on behalf of a Gram user in the gateway. They will be secured by a new **User Sessions** manager that is coordinated by the MCP package.

We will recompose the `oauth` package into two packages:

1. _usersessions_: allow Gram to act as an authorization server for MCP clients and resolve identities to either anonymous sessions or Gram principals
2. _remotesessions_: functionality where Gram acts as a Client for remote Authorization Servers

---

Tracker for the work landing the design in `spike.md`. Per the process in `prompt.md`, this file:

- Names every milestone, its goal, and its scope/non-scope.
- Hosts a `Cleanup` section where we accrete deletion tickets as we discover them.
- Will be populated with per-milestone tickets in step 3 of the process (one sub-agent per milestone). Until then, each milestone's `Tickets` section is a stub.

**Ordering.** Milestones are landed strictly sequentially in the order listed below. Fine-grained dependency tracking lives on tickets within each milestone, not on the milestones themselves.

---

## Milestones

### Milestone #0 — Instrumentation

**Goal.** Add instrumentation to existing OAuth flows so we know with confidence which existing functionality to sunset.

**Scope.**

- Distinguish, in logs and metrics, when `/mcp` traffic uses passthrough vs authenticated vs anonymous sessions.
- Decorate the logger with `mcp_session_id` for the lifetime of any MCP request.
- Log every chat-session-ID issuance and consumption site so we know the surface we must keep backwards-compatible when the JWT shape unifies (spike §4.5).

**Tickets.**

- [ ] Log MCP client capabilities (declared client name/version, advertised features) on every MCP client connection to Gram so we know what's calling and what each caller supports.
- _Further tickets populated in step 3._

---

### Milestone #0b — Mock IDP upgrade

**Status.** 🟡 In progress. Implementation lives on `dev-idp-clean-iwir`; merge to main pending.

**Goal.** Make the mock IDP adequate for end-to-end testing of every authentication mode in this RFC. Full design lives in [`idp-design.md`](./idp-design.md).

**Scope.**

- Replace `mock-speakeasy-idp/` with `gram dev-idp`, a `cmd/gram/` subcommand running four modes simultaneously at `/local-speakeasy/`, `/workos/`, `/oauth2-1/`, `/oauth2/`.
- Postgres-backed (its own logical database `gram_devidp` in the existing `gram-db` container), declarative `schema.sql`, no migration files.
- Authentication is non-interactive across every mode (no login screens, no consent screens) — identity resolves from a per-mode `currentUser` row in the `current_users` table.
- `oauth2-1` and `oauth2` are OIDC-compliant; ID tokens are RS256-signed via an RSA keypair (ephemeral by default; `--rsa-private-key` for stable JWKS).
- Both `local-speakeasy` and `workos` modes serve the Speakeasy IDP wire shape; the difference is identity source (local Postgres vs live WorkOS API).
- Goa design + gen nested under `server/internal/devidp/` (not under `server/design/` or `server/gen/`).
- New top-level project `dev-idp-dashboard` — Hono + React + Vite — for operator-only inspection (no end-user surface).

**Tickets.**

- [ ] **dev-idp server PR.** All Go-side dev-idp work: schema, sqlc queries + repo, Goa management API (organizations / users / memberships / organization_roles / invitations / devIdp), four protocol modes (local-speakeasy / workos / oauth2-1 / oauth2), JWKS + RSA keystore, default-user bootstrap from git committer, `gram dev-idp` cmd entrypoint, mprocs wiring, gram-side `--workos-endpoint` plumbing, and the `testidp` migration that replaces the deleted standalone `mock-speakeasy-idp`.
- [ ] **dev-idp-dashboard PR.** New top-level `dev-idp-dashboard/` project. Operator-only inspection UI: per-mode currentUser, users, organizations, memberships, organization roles, invitations (including accept-flow). Talks to the dev-idp's `/rpc/*` management API; the workos pane uses live data from `/workos/...`.

---

### Milestone #1 — Management APIs for Resources

**Goal.** Land all Postgres schemas from spike §4.1 and expose CRUD for `user_session_issuers`, `user_session_clients`, `user_session_consents`, `user_sessions`, `remote_session_issuers`, `remote_session_clients`, and `remote_sessions` through the management API.

**Scope.**

- All seven Postgres tables from spike §4.1 land in a single migration commit.
- Goa-designed endpoints under `/rpc/<service>.<method>` per the `gram-management-api` skill — the endpoint catalog is spike §6.1 and §6.2.
- Two new packages per spike §3.1: `server/internal/usersessions/` and `server/internal/remotesessions/`. Each composes per-resource handler files (`issuerhandlers.go`, `clienthandlers.go`, etc.) in `impl.go` so tickets scope to one resource at a time.
- Audit logging coverage (per `gram-audit-logging` skill) for every mutation.
- RBAC scopes (per `gram-rbac` skill) gating the new endpoints.

**Tickets.**

- [ ] **`mig:` user/remote session schemas.** Single migration commit landing all seven tables from spike §4.1: `user_session_issuers`, `user_session_clients`, `remote_session_issuers`, `remote_session_clients`, `user_session_consents`, `user_sessions`, `remote_sessions`. FK-ordered within the file. No app code, no `toolsets.user_session_issuer_id` (deferred to Milestone #4). Per `CLAUDE.md` migration rules: ships as its own PR, files generated by `mise db:diff` only.
- [ ] **RBAC scopes + audit subject types bootstrap.** Declare resource types and scope sets for all seven resources (`user_session_issuer`, `user_session_client`, `user_session_consent`, `user_session`, `remote_session_issuer`, `remote_session_client`, `remote_session`) per the `gram-rbac` skill. Declare audit subject types per the `gram-audit-logging` skill. Unblocks every service ticket below.
- [ ] **`usersessions` package skeleton.** New `server/internal/usersessions/`. Goa design files for the four services (`userSessionIssuers`, `userSessionClients`, `userSessionConsents`, `userSessions`). `impl.go` composes empty handler structs from the four `*handlers.go` files. Server wiring per `gram-management-api`. Unblocks the four `usersessions/` handler tickets, which then parallelize.
- [ ] **`usersessions/issuerhandlers.go`.** `userSessionIssuers.{create,update,list,get,delete}` per spike §6.1. Soft-delete cascades app-level to dependent `user_sessions`, `user_session_consents`, and the `remote_session_clients` rows pointing at the issuer. Audit on every mutation.
- [ ] **`usersessions/clienthandlers.go`.** `userSessionClients.{list,get,revoke}` per spike §6.1 — read + revoke only; DCR is the only writer and lands in Milestone #2. `client_secret_hash` is never returned over this surface. Audit on revoke.
- [ ] **`usersessions/consenthandlers.go`.** `userSessionConsents.{list,revoke}` per spike §6.1. Audit on revoke.
- [ ] **`usersessions/sessionhandlers.go`.** `userSessions.{list,revoke}` per spike §6.1 — read + revoke only; sessions are written by `/mcp/{slug}/token` in Milestone #2. `refresh_token_hash` is never returned. Revoke pushes `jti` into the `chat_session_revoked:{jti}` cache (spike §4.5) and soft-deletes the row. Audit on revoke.
- [ ] **`remotesessions` package skeleton.** New `server/internal/remotesessions/`. Goa design files for the three services (`remoteSessionIssuers`, `remoteSessionClients`, `remoteSessions`). `impl.go` composes empty handler structs from the three `*handlers.go` files. Server wiring. Unblocks the three `remotesessions/` handler tickets, which then parallelize.
- [ ] **`remotesessions/issuerhandlers.go`.** `remoteSessionIssuers.{discover,create,update,list,get,delete}` per spike §6.2. `discover` hits the upstream `/.well-known/oauth-authorization-server` (RFC 8414) and returns a draft with `discovery_warnings` — no persistence. `delete` is blocked when any `remote_session_clients` still reference the issuer. Audit on mutations.
- [ ] **`remotesessions/clienthandlers.go`.** `remoteSessionClients.{create,update,list,get,delete}` per spike §6.2. `create` branches on `auto_register`: manual creds are encrypted and stored; `auto_register=true` fires the upstream `registration_endpoint` (per spike §6.5) and persists the result. `client_secret_encrypted` is never returned. Audit on mutations. (Open question per spike §8: split into `.create` (manual) + `.register` (auto)? Resolve before implementation.)
- [ ] **`remotesessions/sessionhandlers.go`.** `remoteSessions.{list,revoke}` per spike §6.2 — read + revoke only; sessions are written by `/mcp/{slug}/remote_login_callback` and the silent-refresh path in later milestones. `access_token_encrypted` and `refresh_token_encrypted` are never returned (only `access_expires_at`, `refresh_expires_at`, `scopes`). Audit on revoke.

---

### Milestone #2 — Token services for user sessions and passthrough principals

**Goal.** Stand up the token-service layer that Milestone #4 will wire into MCP runtime: user-session issuance/validation/refresh/revoke plus passthrough-principal handling, validated with unit tests only.

**Scope.**

- New `server/internal/usersessions/` package per spike §3.1.
- New `server/internal/remotesessions/` token-service seam for passthrough principals per spike §3.1 / §2b.
- Redis types from spike §4.3: `AuthnChallengeState`, `UserSessionGrant`.
- Unified JWT (`SessionClaims`) per spike §4.5; reuses `GRAM_JWT_SIGNING_KEY` and the existing revocation cache.
- User-session TokenService supports `client_secret_basic`, `client_secret_post`, and `none`; all three are advertised in authorization-server metadata. Public clients (`none` + PKCE) are required because real MCP SDKs in the wild use them and bail on AS metadata that doesn't list `none`.
- `/mcp/{slug}/register` writes to `user_session_clients` and logs registration attempts/results with no secret material.
- The `user_session_issuer` package never generates its own session IDs — every method that creates a session accepts one externally (per goal #11 of `prompt.md`).
- Unit test paths: authenticated principal, anonymous principal, passthrough principal.

**Out of scope.**

- No MCP runtime integration yet. Milestone #4 will wire these services into `/mcp` after a focused spike validates the integration shape.
- No `mcp/authn_challenge.go` implementation in this milestone unless needed as a pure unit-test adapter.
- No chain-mode remote OAuth execution; Milestone #3 unit-tests the remotesessions chain primitives.

**Tickets.** _Populated in step 3._

---

### Milestone #3 — Remote sessions, chain mode

**Goal.** Land `remotesessions/` for chain mode: redirect through each remote authn challenge from the Gram callback, build the entire session, then return to the MCP client callback.

**Scope.**

- New `server/internal/remotesessions/` package per spike §3.1.
- Redis types: `RemoteSessionAuthState`, `RemoteSessionPKCE`.
- Consent enforcement: `/mcp/connect` must verify the principal has previously consented for this `user_session_client` to access **all** of the `remote_session_tokens` on its owning `user_session_issuer` (closes the spike §1 / goal #4 vulnerability). Empty `remote_set` is **not** a special case (spike §3.4).
- Consent prompt is shown _somewhere_ in the request stream even though there is no "click each server" UI (spike §2b chain definition).
- Lift token-exchange concerns out of `doHTTP` in the tool proxy (goal #7 of `prompt.md`) — `doHTTP` consumes an opaque credential bundle.

**Tickets.**

- [ ] Design the connect-page UI architecture. v0 ships with vendored Alpine.js for inline interactivity in the `server/internal`-served HTML. Likely needs a separate statically-rendered package as a follow-up — flag the design exploration as its own design doc.
- _Further tickets populated in step 3._

---

### Milestone #4 — URGENT: optional `user_session_issuer` support on `/mcp`

**Goal.** Make `user_session_issuer` an active gating option on `/mcp`. After this milestone, an MCP server configured with a `user_session_issuer` requires a valid Gram-issued user-session JWT for traffic, and that user session can pair with N remote-session credentials forwarded to the backend on tool calls. Generalises today's `oauth_proxy_provider` `type='gram'` case (which is mutually exclusive with `type='custom'` per server) so a Gram login can be combined with upstream OAuth credentials on the same request. Supersedes the previous "toolsets wiring" milestone — folds the FK migration into a runtime opt-in path.

**Scope.**

- Migration adding `toolsets.user_session_issuer_id` (per spike §4.1 "Toolset link"). No removal of the legacy columns yet — expand-contract per the database-migrations rules in `CLAUDE.md`.
- `mcp_servers` mirror column lands alongside whatever `mcp_endpoints` work is happening in parallel (spike §3.5) — may slip into a follow-up depending on timing.
- Runtime: `/mcp/{slug}` consults the configured `user_session_issuer` (if any) and gates traffic on a valid `SessionClaims` JWT.
- Integration tests covering: (a) MCP server with no issuer (legacy path), (b) MCP server with issuer + non-empty remote set. Empty-remote-set is supported by the schema but not a primary scope target.

**Tickets.** _Populated in step 3._

---

### Milestone #5 — Passthrough authentication on `/mcp`

**Goal.** Land the `passthrough` mode end-to-end on `/mcp`: the bearer the MCP client sends is forwarded to the upstream as-is.

**Scope.**

- `passthrough` flag on `remote_session_issuers` is honored end-to-end.
- The MCP client registers directly with the remote AS rather than with Gram.
- We still conform to the `RemoteSession` abstraction even when the bearer is forwarded — i.e. we may still write a remote-session document to keep the system homogeneous (spike §2b).
- Integration tests for the passthrough path against the mock IDP.

**Tickets.** _Populated in step 3._

---

### Milestone #6 — Remote sessions, interactive mode

**Goal.** Add the interactive (multi-plexing) authn challenge UI: Gram issues the user session up front, then renders a connect page where the user clicks each remote OAuth server to authenticate.

**Scope.**

- `/mcp/connect` UI rendering per-remote Connect buttons + Give Access button.
- `interactive` value on `user_session_issuers.authn_challenge_mode` honored end-to-end.
- The same connect screen serves as the consent UI (consent is folded into the Give Access click).
- Shares the underlying state machine with #3 — see `diagrams/unified-authn-challenge-states.mermaid`. Only the trigger mechanism differs.
- This is the screen Milestone #9 will deep-link into.

**Tickets.** _Populated in step 3._

---

### Milestone #7 — Migrate `external_oauth_provider` servers

**Goal.** Move every server currently on the `external_oauth_provider` model onto `user_session_issuer` with **passthrough mode**.

**Scope.**

- Data migration: each `external_oauth_server_metadata` row becomes a `remote_session_issuer` with `passthrough=true` and a paired `user_session_issuer`.
- Update affected toolsets to point at the new `user_session_issuer_id`.
- After this lands, `toolsets.external_oauth_server_id` is dead — track its removal as a Cleanup ticket.

**Tickets.** _Populated in step 3._

---

### Milestone #8 — Migrate `oauth_proxy_server` servers

**Goal.** Move every server currently on the `oauth_proxy_server` model onto the new `user_session_issuer`. This is the heaviest lift because `oauth_proxy_providers` → `remote_session_issuers` + `remote_session_clients` is a real split, not a rename.

**Scope.**

- Data migration: split each `oauth_proxy_provider` row into a `remote_session_issuer` plus a `remote_session_client` (per spike §4.1, §4.2).
- Port each `oauth_proxy_server` row to a `user_session_issuer`.
- Drop the deprecated `oauth_proxy_providers.{secrets, security_key_names, provider_type='custom'}` payload (each becomes its own ticket).
- Update affected toolsets to point at the new `user_session_issuer_id`.

**Tickets.** _Populated in step 3._

---

### Milestone #9 — URL-mode elicitation for stale remote sessions

**Goal.** When a remote session is stale, request fresh credentials via MCP elicitation. The elicitation returns a URL that opens the same connect screen as #6's interactive mode.

**Scope.**

- `RemoteSessionChallenge` state in `diagrams/mcp-handler-states.mermaid` coerces to a URL-mode elicitation when the protocol allows.
- Falls back to a 401 + WWW-Authenticate authn challenge when elicitation isn't available.

**Tickets.** _Populated in step 3._

---

### Milestone #10 — Grant remote sessions to other principals

**Goal.** Let a principal share or delegate a `remote_session` so multiple Gram principals can use the same upstream credentials (e.g. a shared customer-provided OAuth client credential against a shared Notion MCP).

**Scope.** _To be designed._ Out of initial scope but called out here so we don't lose the thread.

**Tickets.** _Populated when this milestone is greenlit._

---

## Cleanup

Tickets to remove dead or about-to-be-dead structure that the new design no longer needs. Each ticket should land as its own PR (separate from feature work).

- [ ] **Remove `AdditionalCacheKeys` from the cached-object interface.**
  - Today's `cache.CacheableObject[T]` interface (`server/internal/cache/cache.go:44`) requires every cached value to declare `AdditionalCacheKeys() []string` so that one logical record can be written under multiple Redis keys. The pattern was introduced for legacy `oauth.Token` (so that the same record was reachable by both access-token-hash and refresh-token-hash).
  - The new schema (spike §4.3) doesn't use multi-key fan-out anywhere. Each record is keyed exactly once. The method is now dead weight on every implementer.
  - **Action:** drop `AdditionalCacheKeys` from the interface; remove the per-implementer stub returns; simplify the cache write paths in `server/internal/cache/cache.go` (lines 68, 129, 152) that iterate the additional keys.

- [ ] **Drop the empty `oauth_proxy_client_info` Postgres table.**
  - `select count(*) from oauth_proxy_client_info` returns 0 in production. The Go code never writes to the table; all DCR registrations go to the Redis key `oauthClientInfo:{mcpURL}:{clientID}`. The table is orphan structure from the original migration.
  - **Action:** drop the table. No data to migrate.

- [ ] **Stop writing `oauthClientInfo:*` Redis entries; let drain.**
  - Once the new DCR path writing to `user_session_clients` (Postgres) ships, stop writing to the legacy Redis key. Existing entries expire with their secrets (TTL = `2 * (ClientSecretExpiresAt - now)`). When the last live entry expires, all DCR traffic is on the new path.
  - **Action:** remove the legacy `clientInfoStorage` write path in `server/internal/oauth/client_registration.go`; keep the read path until we're confident no live entries remain.
