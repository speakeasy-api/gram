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

**Goal.** Make the mock IDP adequate for end-to-end testing of every authentication mode in this RFC. Full design lives in [`idp-design.md`](./idp-design.md).

**Scope.**

- Replace `mock-speakeasy-idp/` with `gram dev-idp`, a `cmd/gram/` subcommand running four modes simultaneously at `/mock-speakeasy/`, `/workos/`, `/oauth2-1/`, `/oauth2/`.
- Postgres-backed (its own logical database `gram_devidp` in the existing `gram-db` container), declarative `schema.sql`, no migration files.
- Authentication is non-interactive across every mode (no login screens, no consent screens) — identity resolves from a per-mode `currentUser` pointer in the `current_users` table.
- `oauth2-1` and `oauth2` are OIDC-compliant; ID tokens are RS256-signed via an RSA keypair (ephemeral by default; `--rsa-private-key` for stable JWKS).
- `workos` mode uniquely proxies the live WorkOS REST API (does not use the dev-idp DB for identity); keyed by `WORKOS_API_KEY` + `WORKOS_HOST`.
- Goa design + gen nested under `server/internal/devidp/` (not under `server/design/` or `server/gen/`).
- New top-level project `dev-idp-dashboard` — Hono package using `@hono/react-renderer` — for operator-only inspection (no end-user surface).

**Tickets.**

_Implementation order matches dependency order; later tickets assume earlier ones have landed._

#### Foundation

- [ ] **mig: dev-idp Postgres schema.** Add `server/internal/devidp/database/schema.sql` with `users`, `organizations`, `memberships`, `current_users`, `auth_codes`, `tokens`. Add `server/internal/devidp/database/sqlc.yaml` and the generated `repo/`. Add `mise db:devidp:apply`, `mise db:devidp:reset`, `mise db:devidp:gen` tasks. Atlas declarative apply only — no `mise db:devidp:diff`.
- [ ] **Compose + zero wiring.** Add `docker-entrypoint-initdb.d/` script creating `gram_devidp` in the existing `gram-db` container. Add `GRAM_DEVIDP_DATABASE_URL` to `mise.toml`. Wire `mise db:devidp:apply` into the `zero` script alongside `mise db:migrate` / `mise clickhouse:migrate`.

#### Goa surface (nested)

- [ ] **Nested Goa API skeleton.** Create `server/internal/devidp/design/api.go` declaring `API("gram-dev-idp", …)`. Add `mise gen:devidp` task: `goa gen .../internal/devidp/design -o internal/devidp` from `server/`. First-cut services: `organizations`, `users`, `memberships`, `devidp` (currentUser get/set). Plain `http.ServeMux` for the four mode handlers — not Goa.

#### Management API impls (Goa)

- [ ] **`organizations.{create,update,list,delete}` impl.** Standard CRUD against the `organizations` table. Cascades to `memberships` on delete.
- [ ] **`users.{create,update,list,delete}` impl.** Standard CRUD. Delete cascades to `memberships`, `auth_codes`, `tokens`, and any `current_users` rows whose `subject_ref` is this user's id.
- [ ] **`memberships.{create,update,list,delete}` impl.** Idempotent on `(user_id, organization_id)` for create.
- [ ] **`devIdp.{getCurrentUser, setCurrentUser}` impl.** Per-mode body shapes — local modes accept `{mode, user_id}`; `workos` accepts `{mode: "workos", workos_sub}`. UPSERTs `current_users`. No `reset` / `resetCurrentUsers` — wipe via `mise db:devidp:reset`.

#### Mode handlers

- [ ] **`mock-speakeasy` mode.** Port `mock-speakeasy-idp/`'s `/v1/speakeasy_provider/*` endpoints under the `/mock-speakeasy/` prefix. Replace the hardcoded `MockUserID` with the mock-speakeasy `currentUser` pointer. Keep the `secret-key` middleware (env: `SPEAKEASY_SECRET_KEY`).
- [ ] **`workos` mode.** Thin REST proxy over `WORKOS_HOST` using `WORKOS_API_KEY`. Endpoints: `/workos/users/{id_or_email}`, `/workos/organizations/{id}`, `/workos/currentUser`. Mode is unmounted when `WORKOS_API_KEY` is unset. **Does not** read `users` / `organizations` / `memberships` from the dev-idp DB.
- [ ] **`mock-speakeasy` ↔ `workos` bridge.** When `WORKOS_API_KEY` is configured, mock-speakeasy resolves user/org metadata via `/workos/currentUser` instead of the local DB. Still issues the speakeasy provider auth code itself.
- [ ] **`oauth2-1` mode.** OAuth 2.1 AS, PKCE-required, stateless DCR, OIDC-compliant. Endpoints: `/.well-known/oauth-authorization-server`, `/.well-known/openid-configuration`, `/.well-known/jwks.json`, `/register`, `/authorize`, `/token`, `/userinfo`, `/revoke`. Accepts any `client_id` / `client_secret`.
- [ ] **`oauth2` mode.** OAuth 2.0 AS, PKCE optional (honored when present), no DCR, OIDC-compliant. Endpoints: same as `oauth2-1` minus `/register`. Accepts any `client_id` / `client_secret`.
- [ ] **JWKS + RSA keypair.** Single dev-idp-wide RSA private key (env: `GRAM_DEVIDP_RSA_PRIVATE_KEY`, otherwise generated at boot). Public key derived via `privateKey.Public()` and served at each OIDC mode's `/.well-known/jwks.json`. **`GRAM_JWT_SIGNING_KEY` is never consumed by dev-idp.**

#### Cmd entrypoint + ops

- [ ] **`server/cmd/gram/dev-idp.go`.** Sibling of `admin.go`. Wires CLI flags (every flag has a backing env var per `idp-design.md` §8), spins up the listener, mounts the Goa management mux + four mode handlers + `/control/healthz`. The binary trusts schema is already applied; errors normally on mismatch.
- [ ] **mprocs wiring.** Add a `dev-idp` proc to `mprocs.yaml` invoking `mise start:dev-idp` (a thin wrapper). Run alongside `mock-idp` during cutover.

#### Cleanup (each its own PR)

- [ ] **Delete `mock-speakeasy-idp/`.** Once no caller references it, remove the standalone binary and its `mprocs.yaml` proc.

#### Dashboard (separate top-level project)

- [ ] **`dev-idp-dashboard` skeleton.** New top-level Hono package using `@hono/react-renderer`. Operator-only — no end-user surface. Renders panes for users / orgs / memberships / tokens / auth codes / per-mode currentUser pointers. The `workos` pane is rendered differently (free-form `workos_sub` input + WorkOS-API "preview" button instead of a local-users picker; lists data live from `/workos/...`).

---

### Milestone #1 — Management APIs for Resources

**Goal.** Land all Postgres schemas from spike §4.1 and expose CRUD for `user_session_issuers`, `user_session_clients`, `user_session_consents`, `user_sessions`, `remote_session_issuers`, `remote_session_clients`, and `remote_sessions` through the management API.

**Scope.**

- All seven Postgres tables from spike §4.1 land here. Each is its own `mig:` commit.
- Goa-designed endpoints under `/rpc/<service>.<method>` per the `gram-management-api` skill — the endpoint catalog is spike §6.1 and §6.2.
- Audit logging coverage (per `gram-audit-logging` skill) for every mutation.
- RBAC scopes (per `gram-rbac` skill) gating the new endpoints.

**Tickets.** _Populated in step 3._

---

### Milestone #2 — User sessions in `x/mcp` (three modes)

**Goal.** Stand up `usersessions/` end-to-end and instrument `x/mcp` with all three principal modes: **authenticated**, **anonymous**, **passthrough**.

**Scope.**

- New `server/internal/usersessions/` package per spike §3.1.
- Redis types from spike §4.3: `AuthnChallengeState`, `UserSessionGrant`.
- Unified JWT (`SessionClaims`) per spike §4.5; reuses `GRAM_JWT_SIGNING_KEY` and the existing revocation cache.
- `mcp/authn_challenge.go` translates `UserSessionManager` errors into MCP authn challenges.
- The `user_session_issuer` package never generates its own session IDs — every method that creates a session accepts one externally (per goal #11 of `prompt.md`).
- Three integration test paths: authenticated, anonymous, passthrough.

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
