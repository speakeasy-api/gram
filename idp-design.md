# IDP Design — `gram dev-idp`

> Scope of this doc: design of the upgraded mock IDP that satisfies milestone
> #0b in `project.md`. Sibling artifact to `spike.md`. Captures package
> structure and endpoint surface only — flows live in the milestone-#0b
> diagrams under `/diagrams/idp-*.mermaid` (TBD).
>
> **For implementers picking this up cold:** read in this order — `prompt.md`
> (project context) → §1–§3 here (purpose, modes, identity model) → §4–§7
> (layout, schema, mgmt API, per-mode endpoints) → §8–§9 (CLI, cutover).
> §10 is "out of scope" and §11 is **fully resolved** — kept as a paper
> trail of decisions so you can see why each choice was made. The ticket
> list is in `project.md` under "Milestone #0b — Mock IDP upgrade" and is
> ordered for landing.

---

## 1. Why this exists

Today's mock IDP (`/mock-speakeasy-idp/`) speaks **one** protocol — the
Speakeasy auth-provider exchange — and supports one in-memory user. That is
adequate for spinning up Gram locally but is **not** adequate for end-to-end
testing of the new auth surfaces this RFC introduces:

- **`user_session_issuer`** must be exercised against a Gram-side AS (the
  Speakeasy IDP today; potentially a real OIDC provider tomorrow).
- **`remote_session_issuer`** must be exercised against a **third-party** AS
  speaking either OAuth 2.0 or OAuth 2.1 + PKCE + DCR.
- The existing **WorkOS** integration must still resolve identities for dev
  workflows that go through the real auth flow.

`dev-idp` upgrades the mock so a single binary can stand in for **all** of
these IDPs simultaneously, backed by one shared Postgres store of users,
organizations, and applications, and provides the test hooks (currentUser
selection, app/user/org seeding via management API) the integration suite
needs.

It absorbs `mock-speakeasy-idp` — the existing `/v1/speakeasy_provider/*`
surface moves under the new `mock-speakeasy` mode and the standalone binary
goes away once we cut over.

---

## 2. Modes

Each mode is mounted at a fixed URL prefix on the same listener. All four
share a single Postgres store (§5).

| Prefix             | Protocol                                                                                   | What it backs in tests                                                                                                                                                                      |
| ------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/mock-speakeasy/` | Speakeasy IDP bridge (secret-key authed exchange/validate)                                 | Drop-in replacement for today's `mock-speakeasy-idp`. Powers Gram management-API login during local dev.                                                                                    |
| `/workos/`         | Thin proxy over the real WorkOS REST API (configured with a WorkOS API key). **Not OIDC.** | Standalone identity universe: tests/dashboard that need a real WorkOS user/org pin a WorkOS `sub` via `current_users[mode='workos']`. No browser redirects, no `/authorize`, no `/token`.   |
| `/oauth2-1/`       | OAuth 2.1 AS — PKCE required, DCR enabled (stateless), **OIDC-compliant**                  | Backs `remote_session_issuer` rows used in chain / interactive remote-session tests (spike §5.2 / §5.3). Also a candidate `user_session_issuer` upstream once we wire that flow end-to-end. |
| `/oauth2/`         | OAuth 2.0 AS — PKCE supported (optional, not required), no DCR, **OIDC-compliant**         | Backs the legacy `remote_session_issuer` shape for migration testing of currently-existing customer servers (milestone #7 / #8).                                                            |

The mode set is deliberately small but extensible. Adding a mode is a new
sub-package under `server/internal/devidp/modes/` and one line in the
top-level handler dispatch table.

---

## 3. Stateful `currentUser` — and why authentication is never interactive

**Authentication in dev-idp is always automatic, never interactive.** No
mode ever renders a login form, password prompt, or consent screen. Every
`/authorize`-equivalent endpoint resolves identity from a per-mode
**`currentUser`** and immediately redirects with the issued code. This is
a non-negotiable design property — it is what makes the dev-idp usable
from non-browser integration tests and headless CI.

The currentUser is **per mode** (per issuer type), not global:

```
currentUser = {
  "mock-speakeasy": users.id,
  "workos":         users.id,
  "oauth2-1":       users.id,
  "oauth2":         users.id,
}
```

The motivation isn't multi-principal flows for their own sake — it's that
the **MCP traffic and the management-API traffic in the same Gram process
should be allowed to belong to different users**. If currentUser were
global, hitting `/mcp/{slug}` as Alice while a dashboard tab in the same
browser is signed in as Bob would force a contradiction; one of the two
sessions would have to be torn down. Splitting per issuer type sneaks
around that requirement: `mock-speakeasy`'s currentUser governs
management-API login (Bob), `oauth2-1`'s currentUser governs the MCP-side
upstream OAuth identity (Alice), and the two never have to agree.

- Each currentUser is a **subject ref** persisted in the `current_users`
  Postgres table (§5). The string semantics are mode-specific:
  - For `mock-speakeasy` / `oauth2-1` / `oauth2`: a `users.id` UUID into
    the local `users` table.
  - For `workos`: a **WorkOS `sub`** (the WorkOS user id, e.g.
    `user_01H...`) — **not** a ref to the local `users` table. The
    workos mode never reads `users` / `organizations` / `memberships`;
    its identity universe is the live WorkOS account behind
    `--workos-api-key`. See §7.2.
- Settable via `POST /rpc/devIdp.setCurrentUser` (§6.2); readable via
  `GET /rpc/devIdp.getCurrentUser`. Both methods take a `mode` parameter
  and a mode-appropriate body.
- **A mode with no `current_users` row falls back to a default user
  derived from the local git committer.** For local modes
  (`mock-speakeasy`/`oauth2-1`/`oauth2`), the dev-idp shells out to
  `git config user.{email,name}`, find-or-creates a row in `users` for
  that email, places the user in a "Speakeasy" organization
  (find-or-created by slug), and persists `current_users[mode]` so the
  bootstrap fires at most once per mode per dev-idp database. For
  `workos`, the committer email is looked up live via
  `GetUserByEmail`; the resulting WorkOS sub is persisted the same way.
  Either path errors with a message naming committer email/name as the
  default-user source if `git config` is unset (or, for workos, if
  WorkOS rejects the lookup). Operators wanting a non-default user
  override via `setCurrentUser`. Once set, the row persists across
  dev-idp restarts; wipe via `mise db:devidp:reset`.

There is no `auto_consent` flag, no "auto bypass" toggle, no consent
record table — those are all knobs you only need if interactive auth is
in scope, and it isn't.

Because the currentUser is global per dev-idp process (just sharded by
mode), parallel tests sharing one dev-idp binary need to coordinate or
stand up their own instance. We do not introduce a per-request override
header — that surface risks leaking into outbound `doHTTP` calls and is
not worth the fragility.

---

## 4. Package layout

```
server/cmd/gram/
  dev-idp.go                  (1 file, sibling of start.go / worker.go / admin.go)
                              — declares newDevIdpCommand(); wires CLI flags;
                                spins up the dev-idp HTTP listener; mounts both
                                the Goa management mux and the per-mode
                                protocol handlers; mirrors admin.go layout.

server/internal/devidp/                ← everything dev-idp lives under this tree
  design/                              ← Goa DSL, NESTED here (not under server/design/)
    api.go                  — declares Goa API gram-dev-idp, distinct from production gram API
    organizations.go
    users.go
    memberships.go
    devidp.go
  gen/                                 ← Goa codegen output, NESTED here (not under server/gen/)
    devidp/                 — generated service stubs
    organizations/
    users/
    memberships/
  database/
    schema.sql              — declarative Postgres SDL, embedded at build time
    queries.sql             — sqlc input
    sqlc.yaml               — sqlc config (engine: postgresql)
    repo/                   — sqlc-generated Go
    db.go                   — pg connection bootstrap; trusts that schema.sql
                              has already been applied via mise db:devidp:apply.
                              Errors on schema mismatch.
  service/
    organizations.go        — Goa impl for /rpc/organizations.*
    users.go                — Goa impl for /rpc/users.*
    memberships.go          — Goa impl for /rpc/memberships.*
    devidp.go               — Goa impl for /rpc/devIdp.* (currentUser get/set)
  modes/
    mockspeakeasy/handler.go — secret-key authed /v1/speakeasy_provider/* endpoints
    workos/handler.go        — thin proxy over the real WorkOS REST API,
                                configured with --workos-api-key. Not OIDC.
    oauth2-1/handler.go      — /authorize (PKCE required), /token, /register, /jwks
    oauth2/handler.go        — /authorize (PKCE optional), /token, /jwks
  handler.go                — composes all four mode handlers under their prefixes
                              and the Goa management mux at /rpc/

server/cmd/gram/
  dev-idp.go                  (1 file, sibling of start.go / worker.go / admin.go)
                              — declares newDevIdpCommand(); wires CLI flags;
                                spins up the dev-idp HTTP listener; mounts both
                                the Goa management mux and the per-mode
                                protocol handlers; mirrors admin.go layout.
                                Imports devidp.Handler() and devidp/gen/* —
                                both internal-to-`server/` so the import is fine.

dev-idp-dashboard/            — separate top-level project. Hono package using
                              @hono/react-renderer. Operator-only surface — lists
                              users / orgs / tokens, shows the per-mode
                              currentUser, exposes a "switch to user X"
                              button. Does NOT render any login or consent
                              screen (auth is always non-interactive — §3).
                              Designed and tracked separately under milestone
                              #0b.
```

Notes on the layout:

- The cmd-level file `server/cmd/gram/dev-idp.go` is intentionally thin —
  flags + dependency wiring + listener — matching the `start.go` / `worker.go`
  / `admin.go` convention. All real logic lives under
  `server/internal/devidp/`.
- Mode packages **only** depend on `database/repo`. They do not import
  each other. Adding a new mode is a new sub-directory under `modes/`
  plus one line in `handler.go`.
- `server/internal/devidp/` re-uses `attr`, `o11y`, `middleware`, `control`,
  and the `cli` flag-loader from the rest of `cmd/gram/`. It does **not**
  depend on the production Postgres schema, the production design package,
  or any production service implementation.
- The `design/` and `gen/` directories are **nested inside** the dev-idp
  package rather than living under `server/design/` and `server/gen/`
  alongside the production stack. Rationale in §6.3.

### 4.1 What we re-use from `server/`

| Concern                | Source                                                      |
| ---------------------- | ----------------------------------------------------------- |
| CLI framework          | `urfave/cli` via `server/cmd/gram` patterns                 |
| Logger / OTel setup    | `server/internal/o11y`, `server/internal/attr`              |
| Health / control plane | `server/internal/control`                                   |
| HTTP middleware        | `server/internal/middleware`                                |
| API design framework   | Goa (same DSL, separate API definition — §6.3)              |
| Schema-as-SDL pattern  | declarative Postgres `schema.sql` + sqlc (per `CLAUDE.md`)  |
| Code generation        | sqlc (postgresql engine, same as production)                |
| Schema apply tool      | atlas declarative apply (same tool, no migration files out) |

### 4.2 What we deliberately do **not** re-use

| Concern                                  | Why                                                                                                                                                                        |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Production `server/database/schema.sql`  | dev-idp has its own narrow schema (§5). No tables overlap with production; the two databases must stay isolated.                                                           |
| Production `server/migrations/`          | dev-idp's schema is regenerated declaratively on boot; we never produce a migration file for it.                                                                           |
| Production `server/design/`              | dev-idp gets its own Goa API definition, **nested inside** the dev-idp package (§6.3). No collision with production codegen and no entries in the production design index. |
| `authz.Engine`, `sessions.Manager`, etc. | dev-idp impersonates external IDPs — it must not depend on Gram's auth stack to validate that stack.                                                                       |

---

## 5. Schema (Postgres)

Postgres, declarative `server/internal/devidp/database/schema.sql`. The
file is embedded into the binary. On startup, `database.Open(ctx)` connects
to the configured Postgres URL and uses **atlas declarative apply** to bring
the live schema in line with the embedded SDL. **No migration files are
written.** Schema changes ship as edits to `schema.sql` plus regenerated
sqlc.

The dev-idp expects to talk to its **own** Postgres database (separate
`--database-url` from the main server). Pointing it at the production
database is unsupported — atlas declarative apply would happily drop tables
it doesn't recognise.

| Table           | Purpose                                                                                                                                                                                                                                                                                                                                                                             |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `users`         | Identity rows. `id`, `email`, `display_name`, `photo_url?`, `github_handle?`, `admin`, `whitelisted`, timestamps.                                                                                                                                                                                                                                                                   |
| `organizations` | Org records. `id`, `name`, `slug`, `account_type`, `workos_id?`, timestamps. Consumed by `mock-speakeasy` (the `workos` mode reads its identity entirely from live WorkOS, not this table).                                                                                                                                                                                         |
| `memberships`   | `(user_id, organization_id)` join with `role` (default `member`).                                                                                                                                                                                                                                                                                                                   |
| `current_users` | Per-mode `currentUser` (§3). `(mode TEXT PK, subject_ref TEXT NOT NULL, updated_at)`. `subject_ref` is mode-specific: `users.id` UUID for `mock-speakeasy`/`oauth2-1`/`oauth2`; WorkOS `sub` for `workos`. **No FK** because the workos value is external. **Not seeded** — row appears the first time `setCurrentUser` is called for that mode. Modes return an error before that. |
| `auth_codes`    | Short-TTL `/authorize` codes. `(code, mode, user_id, client_id, redirect_uri, code_challenge?, code_challenge_method?, scope?, expires_at)`. `client_id` is **recorded for inspection only** — see §5.2.                                                                                                                                                                            |
| `tokens`        | Issued access / id / refresh tokens. `(token, mode, user_id, client_id, kind, scope?, expires_at, revoked_at?)`. `client_id` is recorded for inspection only.                                                                                                                                                                                                                       |

### 5.1 Cross-mode sharing

`users`, `organizations`, `memberships` are **mode-agnostic** — every mode
resolves identities through them. This is the "same core configuration of
users" the prompt called for, and the only state the management API in §6
exposes.

`auth_codes` and `tokens` carry a `mode` discriminator — issuance and
validation in one mode never sees rows belonging to another mode. Per-mode
behaviour (token shape, expiry policy) stays strictly local while the user
pool stays global.

### 5.2 No `applications` table — register is stateless and clients are unauthenticated

There is no `applications` table and no `/rpc/applications.*` surface.
**Every mode accepts every `client_id` and every `client_secret`,
regardless of whether they have ever been "registered."**

- `oauth2-1/register` (RFC 7591 DCR) is **stateless**: it generates a
  random `client_id` + `client_secret`, returns them, and persists nothing.
  Subsequent `/authorize` and `/token` calls accept that `client_id` the
  same way they would accept any other — no lookup, no validation.
- `client_secret_basic` at `/token` (oauth2 / oauth2-1) accepts any
  secret. The mode does not check it against anything.
- `redirect_uri` is echoed back to the caller without being checked
  against a registered list — the caller's request is the source of truth.

This is intentional. Authenticating clients adds significant test setup
(register first, then exchange) without exercising any production code
path that the dev-idp is here to support. Production Gram exercises the
_issuer_ side of client validation against real upstreams; the dev-idp is
here to act _as_ the upstream, and acting permissively is closer to the
spirit of "test the integration" than refusing requests on bookkeeping
grounds would be.

`client_id` still gets recorded on `auth_codes` and `tokens` so the
dashboard can show "this token was issued to client X" — but it is
metadata, never a constraint.

### 5.3 What is **not** in the schema

- **`applications`.** Stateless register — see §5.2.
- **Persistent consent records.** Auth is always non-interactive (§3) —
  no consent ever fires.
- **Signing keys.** dev-idp owns a single RSA private key (configurable via
  `--rsa-private-key`, otherwise generated ephemerally at boot). It is the
  **only** signing key the dev-idp uses; nothing in dev-idp ever consumes
  the production `GRAM_JWT_SIGNING_KEY` (HS256). The public key is **not**
  configured separately — it's derived from the private key at boot
  (`privateKey.Public()`) and formatted as a JWK on demand at each mode's
  `/.well-known/jwks.json`. Use of the keypair:
  - OIDC `id_token`s issued by `/oauth2-1/` and `/oauth2/` are RS256-signed
    with the private key. The derived public key is served at each mode's
    JWKS endpoint.
  - Access tokens, refresh tokens, and `mock-speakeasy` id tokens are
    **opaque random bytes**, looked up server-side via the `tokens` table.
    They are not JWTs and need no signing key. (This matches today's
    `mock-speakeasy-idp` behaviour and keeps JWKS the only verification
    surface tests have to wire up.)

### 5.4 Atlas, mise tasks, and `zero` integration

The dev-idp's schema is applied **declaratively** with `atlas schema apply`
(not the `atlas migrate diff` / `atlas migrate apply` pair the production
server uses). No migration files are ever written. Drift between the live
schema and `schema.sql` is resolved by re-running apply.

New tasks under `.mise-tasks/db/devidp/`:

| Task                   | Behaviour                                                                                                                                                                                                                                                                                                                                                                        |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `mise db:devidp:apply` | `atlas schema apply --url $GRAM_DEVIDP_DATABASE_URL --to file://server/internal/devidp/database/schema.sql --dev-url docker://pgvector/pgvector/pg17/dev?search_path=public --auto-approve`. **Operator must run this** before `gram dev-idp` is useful — the binary does not shell out to atlas itself; it just connects and errors normally if the schema is missing or stale. |
| `mise db:devidp:reset` | `DROP SCHEMA public CASCADE; CREATE SCHEMA public;` against the dev-idp DB, then `mise db:devidp:apply`. Local-only, parallels the existing `mise db:reset`. Wipes `current_users` along with everything else.                                                                                                                                                                   |
| `mise db:devidp:gen`   | Runs `sqlc generate` against `server/internal/devidp/database/sqlc.yaml`, regenerates `server/internal/devidp/database/repo/`. Mirrors the existing `mise db:gen` for the production server.                                                                                                                                                                                     |
| `mise db:devidp:diff`  | **Intentionally absent.** dev-idp does not produce migration files.                                                                                                                                                                                                                                                                                                              |

`compose.yml` gets a new init script under
`docker-entrypoint-initdb.d/` that creates a second logical database
(`gram_devidp`) inside the existing `gram-db` Postgres container. No new
container needed; the dev-idp connects to the same Postgres instance with
a different `--database-url`.

`zero` integration:

- `zero` (and `zero --agent`) currently runs `mise db:migrate` and
  `mise clickhouse:migrate`. Add `mise db:devidp:apply` to the same block
  so a fresh checkout's dev-idp database is materialised on first
  bootstrap.
- `mprocs.yaml` gets a new `dev-idp` proc invoking `mise start:dev-idp`
  (a thin wrapper around `gram dev-idp`). `madprocs` starts it alongside
  `mock-idp` / `server` / `worker` / `dashboard` / `elements` during the
  cutover; once `mock-idp` is removed (§9, step 5) it is the sole IDP
  proc.
- `mise.toml` gets a `GRAM_DEVIDP_DATABASE_URL` env entry pointing at the
  new logical database in the local container.

---

## 6. Management API (Goa)

The dev-idp exposes a Goa-designed management API at `/rpc/<service>.<method>`.
Two layers:

- **§6.1 — Resource CRUD** for `organizations`, `users`, and `memberships`.
  The dashboard, integration-test setup helpers, and CLI seed scripts are
  the consumers. Notably absent: `applications` (§5.2).
- **§6.2 — Dev-only RPC** for the currentUser, the read-only
  applications view, and the test-reset hook.

All endpoints are POST to `/rpc/<service>.<method>` (Gram convention).
Pagination on `list` follows the standard cursor convention:
`cursor` (string, optional) + `limit` (int, default `50`, max `100`),
response carries a `next_cursor` (empty when exhausted).

The whole management surface is **permanently unauthenticated** —
dev-idp is a localhost-only tool and reaching it from anywhere else
means something has gone wrong upstream. No boot-token header, no API
key.

### 6.1 Resource CRUD

#### `organizations.create`

| Field          | Type   | Required | Notes                                      |
| -------------- | ------ | -------- | ------------------------------------------ |
| `name`         | string | required |                                            |
| `slug`         | string | required | unique                                     |
| `account_type` | string | optional | default `free`                             |
| `workos_id`    | string | optional | echoed by `/mock-speakeasy/` validate flow |

Returns full `Organization` record.

#### `organizations.update`

| Field          | Type   | Required | Notes                  |
| -------------- | ------ | -------- | ---------------------- |
| `id`           | uuid   | required |                        |
| `name`         | string | optional |                        |
| `slug`         | string | optional |                        |
| `account_type` | string | optional |                        |
| `workos_id`    | string | optional | clear via empty string |

#### `organizations.list`

Standard pagination. Response: `{ items: Organization[], next_cursor: string }`.

#### `organizations.delete`

| Field | Type | Required | Notes       |
| ----- | ---- | -------- | ----------- |
| `id`  | uuid | required | hard delete |

Cascades to `memberships`.

#### `users.create`

| Field           | Type   | Required | Notes           |
| --------------- | ------ | -------- | --------------- |
| `email`         | string | required | unique          |
| `display_name`  | string | required |                 |
| `photo_url`     | string | optional |                 |
| `github_handle` | string | optional |                 |
| `admin`         | bool   | optional | default `false` |
| `whitelisted`   | bool   | optional | default `true`  |

Returns full `User` record.

#### `users.update`

`id` required; every other field optional patch.

#### `users.list`

Standard pagination.

#### `users.delete`

| Field | Type | Required |
| ----- | ---- | -------- |
| `id`  | uuid | required |

Cascades to `memberships`, `auth_codes`, `tokens`, and any `current_users`
rows whose `subject_ref` is this user's id (the affected modes will start
returning the "no currentUser set" error from §3 until an operator
re-points them).

#### `memberships.create`

| Field             | Type   | Required | Notes            |
| ----------------- | ------ | -------- | ---------------- |
| `user_id`         | uuid   | required |                  |
| `organization_id` | uuid   | required |                  |
| `role`            | string | optional | default `member` |

Returns the `Membership` record. Idempotent on `(user_id, organization_id)`.

#### `memberships.update`

| Field  | Type   | Required | Notes |
| ------ | ------ | -------- | ----- |
| `id`   | uuid   | required |       |
| `role` | string | required |       |

#### `memberships.list`

Standard pagination. Optional filters: `user_id`, `organization_id`.

#### `memberships.delete`

| Field | Type | Required |
| ----- | ---- | -------- |
| `id`  | uuid | required |

**No `/rpc/applications.*` surface.** Applications are stateless — see
§5.2. There is nothing to manage and nothing to list.

### 6.2 Dev-only RPC

Mounted under `/rpc/devIdp.*`. Same Goa API, separate service. Reads and
writes the `current_users` table (§5).

| Method                  | Purpose                                                                                                                                                                                                                                                                                                                                                                |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `devIdp.getCurrentUser` | Returns the currentUser for a mode. Body: `{mode}`. Response shape varies by mode: local modes (`mock-speakeasy`/`oauth2-1`/`oauth2`) return the full `users` row from the dev-idp DB; `workos` mode returns `{workos_sub, …live workos profile fields…}` resolved via the WorkOS API (§7.2). Returns 404 when no row exists yet for that mode.                        |
| `devIdp.setCurrentUser` | UPSERTs `current_users` for a mode. Body shape per mode: local modes accept `{mode, user_id}` (a UUID into the local `users` table — fetch first via `users.list` if you only have the email); `workos` accepts `{mode: "workos", workos_sub}` (the literal WorkOS user id, no DB validation — the workos mode trusts the operator). Persists across dev-idp restarts. |

There is **no** `devIdp.reset` and no `devIdp.resetCurrentUsers`. If a
test really wants a clean slate, the way to get one is
`mise db:devidp:reset` (§5.4) — schema-level wipe of the whole dev-idp DB.
We don't ship in-band reset RPCs because the surface area they protect
against (cross-test pollution) is something integration tests should
manage by writing their own fixtures, not by leaning on a dev-idp
"forget everything" button.

### 6.3 Goa wiring — separate API, nested under the dev-idp package

The dev-idp's Goa surface is its **own API** named `gram-dev-idp`, declared
in `server/internal/devidp/design/api.go`. **Both the design DSL and the
Goa-generated code live nested under `server/internal/devidp/`**, not
under the global `server/design/` and `server/gen/` directories the
production server uses.

Why nest:

- **Encapsulation.** The dev-idp is a self-contained sub-binary. Its
  generated handlers and types have no consumers outside
  `server/internal/devidp/`. Putting them under `internal/` makes that an
  enforced property — Go's `internal/` rule prevents anything outside
  `server/` from importing them, and _nothing_ outside `cmd/gram/dev-idp.go`
  needs to.
- **No pollution of the production design index.** `server/design/gram.go`
  blank-imports every production design package; adding `design/devidp`
  there would risk accidentally surfacing dev-idp endpoints from the
  production binary. Nesting keeps the two design trees physically
  unable to collide.
- **Trivially deletable.** When dev-idp goes away (someday), one
  `rm -rf server/internal/devidp/` removes the entire footprint —
  design, gen, schema, code. No grep across `server/design/` and
  `server/gen/` to find stragglers.
- **No `/rpc/users.*` collision.** The dev-idp's `users` Goa service and
  the production `users` (whenever production grows one) are in different
  Go packages by virtue of different import paths (`.../devidp/gen/users`
  vs `.../gen/users`). This was the original collision concern and it
  goes away "for free" with nesting.

Codegen invocation (mise task `gen:devidp`, run from `server/`):

```bash
goa gen github.com/speakeasy-api/gram/server/internal/devidp/design \
  -o internal/devidp
```

Goa's `-o` flag appends `gen/` to the directory you give it, so this
writes everything under `server/internal/devidp/gen/`. The dev-idp cmd
imports `.../internal/devidp/gen/...`; the production server never
imports any of it.

---

## 7. Protocol surface — per mode

Each mode is a plain `http.ServeMux` (not Goa — OAuth flows are spec-shaped,
not RPC-shaped). Each mode reads from the §6.1-managed resources to resolve
identities, but **does not validate inbound `client_id` / `client_secret`**
(§5.2) — every mode is permissive on the client side.

### 7.1 `/mock-speakeasy/`

Drop-in replacement for `/v1/speakeasy_provider/*` from
`mock-speakeasy-idp/`. Endpoints retain their existing paths under the new
prefix:

- `GET  /mock-speakeasy/v1/speakeasy_provider/login`
- `POST /mock-speakeasy/v1/speakeasy_provider/exchange`
- `GET  /mock-speakeasy/v1/speakeasy_provider/validate`
- `POST /mock-speakeasy/v1/speakeasy_provider/revoke`
- `POST /mock-speakeasy/v1/speakeasy_provider/register`

**Resources consumed:**

| Endpoint    | Reads                                               | Writes                                                |
| ----------- | --------------------------------------------------- | ----------------------------------------------------- |
| `/login`    | `users` (resolves currentUser for `mock-speakeasy`) | `auth_codes` (mode=`mock-speakeasy`)                  |
| `/exchange` | `auth_codes`                                        | `tokens` (mode=`mock-speakeasy`, kind=`id_token`)     |
| `/validate` | `tokens`, `users`, `memberships`, `organizations`   | —                                                     |
| `/revoke`   | `tokens`                                            | `tokens.revoked_at`                                   |
| `/register` | `tokens`                                            | `organizations`, `memberships` (creates org for user) |

Behaviour matches the existing impl: the auto-login form binds the
mock-speakeasy `currentUser` (no longer a hard-coded `MockUserID`),
and the `secret-key` middleware accepts `SPEAKEASY_SECRET_KEY` from env or
flag. mock-speakeasy is a fully local mode — its identity universe is the
dev-idp's `users` / `organizations` / `memberships` tables. To exercise
WorkOS-backed identity end-to-end, point Gram at the `/workos/` mode
(§7.2) directly; the per-mode `currentUser` design (§3) keeps the two
identity universes independent on purpose.

### 7.2 `/workos/`

**Not OIDC, and not backed by the dev-idp DB.** This mode is the one
exception to the "all modes share the cross-mode store" framing in §5.1.
It is a thin proxy over the real WorkOS REST API and the only state it
carries in the dev-idp DB is its `current_users` row (§5), which stores a
**WorkOS `sub`** rather than a `users.id`.

Configured by two env vars (both with backing flags):

- `--workos-api-key` / `WORKOS_API_KEY` — required. Empty unmounts the
  mode entirely.
- `--workos-host` / `WORKOS_HOST` — base URL of the WorkOS API (default
  `https://api.workos.com`). Override for staging / sandbox / a recorded
  fixture host.

Why no DB and no OIDC: in dev-idp **nobody is authenticating** (§3). For
the local modes we model identity by stamping a row out of the local
`users` table; for workos we model it by _naming an actual workos user_.
Round-tripping through OIDC just to look up a user record is wasted
machinery, and reflecting WorkOS users into our local `users` table would
either fall out of date or require a sync loop. Instead the workos mode
**directly proxies WorkOS read endpoints** with the configured API key
and lets the operator name the user via WorkOS's own identifiers.

#### Endpoints

- `GET /workos/users/{id_or_email}` — `users.get` (when the segment is a
  `user_…` id) or `users.list?email=` (when it parses as an email)
  passthrough.
- `GET /workos/organizations/{id}` — `organizations.get` passthrough.
- `GET /workos/currentUser` — convenience endpoint: looks up the
  `current_users` row for `mode=workos` and returns the same payload as
  `/workos/users/{sub}`.

A request to any of these hits `${WORKOS_HOST}/...` with the API key and
proxies the JSON back unchanged. No caching, no local persistence beyond
the `current_users` row.

#### Setting the workos currentUser

`devIdp.setCurrentUser{mode: "workos", workos_sub: "user_01H..."}`
writes the sub to `current_users` (§6.2). dev-idp does **not** validate
the sub against WorkOS at write time — set whatever you want, and the
first call to `/workos/currentUser` will surface a real error if WorkOS
rejects it. This keeps the management API offline-tolerant.

#### Dashboard surface

The dashboard renders the workos mode differently from the rest:

- The "switch to user X" picker for local modes is a list pulled from
  `/rpc/users.list`. For workos it's a free-form `workos_sub` input plus
  a "preview" button that hits `/workos/users/{sub}` to confirm the user
  exists in WorkOS before saving.
- The user/org tables in the workos pane render data fetched live from
  `/workos/...` rather than from the local `/rpc/users.list` /
  `/rpc/organizations.list`.

**Resources consumed:**

| Endpoint               | Reads                                                       | Writes |
| ---------------------- | ----------------------------------------------------------- | ------ |
| `/users/{id_or_email}` | WorkOS API (live), `--workos-api-key`, `--workos-host`      | —      |
| `/organizations/{id}`  | WorkOS API (live), `--workos-api-key`, `--workos-host`      | —      |
| `/currentUser`         | `current_users` (workos row), then WorkOS API for live data | —      |

If `--workos-api-key` is unset, the `workos` mode is not mounted at all.
The other three modes are unaffected — each carries its own per-mode
`currentUser` (§3) and never reads from the others.

### 7.3 `/oauth2-1/`

OAuth 2.1 AS, PKCE-required, DCR-enabled, **OIDC-compliant** (advertises an
OpenID Provider metadata document). Backs `remote_session_issuer` rows used
in remote-session tests.

- `GET  /oauth2-1/.well-known/oauth-authorization-server` — RFC 8414 AS metadata.
- `GET  /oauth2-1/.well-known/openid-configuration` — OpenID Connect Discovery 1.0 metadata. Same underlying config as the RFC 8414 doc, plus the OIDC-required fields (`subject_types_supported`, `id_token_signing_alg_values_supported`, `userinfo_endpoint`, `claims_supported`). Issued so future tests can treat this mode as a real OIDC IdP without protocol-level changes.
- `POST /oauth2-1/register` — RFC 7591 DCR. **Stateless** — generates and returns a `client_id` + `client_secret` and persists nothing (§5.2).
- `GET  /oauth2-1/authorize` — PKCE required (S256). Auto-passes for currentUser. Accepts any `client_id` without lookup. When the request includes `scope=openid`, the `/token` response includes an `id_token`.
- `POST /oauth2-1/token` — `authorization_code` (with `code_verifier`) and `refresh_token` grants. Accepts any `client_id` / `client_secret`. Issues `id_token` when `openid` scope was requested.
- `GET  /oauth2-1/userinfo` — OIDC userinfo endpoint. Returns the standard claim set (`sub`, `email`, `name`, `picture`) for the bearer's user.
- `POST /oauth2-1/revoke` — RFC 7009.
- `GET  /oauth2-1/.well-known/jwks.json` — JWKS for ID-token signing keys.

**Resources consumed:**

| Endpoint                                  | Reads                                    | Writes                                                                                              |
| ----------------------------------------- | ---------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `/.well-known/oauth-authorization-server` | —                                        | —                                                                                                   |
| `/.well-known/openid-configuration`       | —                                        | —                                                                                                   |
| `/register`                               | —                                        | — (stateless; nothing persisted)                                                                    |
| `/authorize`                              | `users` (oauth2-1 currentUser)           | `auth_codes` (mode=`oauth2-1`, records `client_id` + `code_challenge` + `scope`)                    |
| `/token`                                  | `auth_codes` (validates `code_verifier`) | `tokens` (mode=`oauth2-1`, kinds=`access_token` + `refresh_token` [+ `id_token` if `openid` scope]) |
| `/userinfo`                               | `tokens`, `users`                        | —                                                                                                   |
| `/revoke`                                 | `tokens`                                 | `tokens.revoked_at`                                                                                 |
| `/.well-known/jwks.json`                  | —                                        | —                                                                                                   |

### 7.4 `/oauth2/`

OAuth 2.0 AS — **PKCE supported but not required**, no DCR,
**OIDC-compliant** (same OpenID Provider metadata surface as `oauth2-1`).
`client_secret_basic` shape at the token endpoint, accepted-but-not-validated
(§5.2). Backs the legacy `remote_session_issuer` shape used in migration
tests (milestones #7 / #8).

The PKCE-supported choice is deliberate. PKCE is widely deployed against
real OAuth 2.0 issuers in 2026, and we want to exercise the upstream-side
PKCE handling in `remotesessions/` against a 2.0-shaped issuer too — not
just against `/oauth2-1/`. Tests that want to exercise the no-PKCE path
simply omit `code_challenge` from the `/authorize` request; the mode then
honours the legacy "no `code_verifier` required" path at `/token`.

OIDC compliance on this 2.0-shaped mode is also deliberate, even though
no current test consumes it: it future-proofs the mode for any milestone
that wants to assert "we behave correctly against an OIDC issuer that
isn't 2.1," without our having to add a fifth mode later.

- `GET  /oauth2/.well-known/oauth-authorization-server` — RFC 8414. Advertises `code_challenge_methods_supported: ["S256"]`.
- `GET  /oauth2/.well-known/openid-configuration` — OpenID Connect Discovery 1.0 metadata.
- `GET  /oauth2/authorize` — auth code grant. `code_challenge` is **optional**. Accepts any `client_id`. Honours `scope=openid`.
- `POST /oauth2/token` — `authorization_code` + `refresh_token`. If the auth code was minted with `code_challenge`, the matching `code_verifier` is required; otherwise `code_verifier` is ignored. Accepts any `client_secret_basic` header. Issues `id_token` when `openid` scope was requested.
- `GET  /oauth2/userinfo` — OIDC userinfo endpoint.
- `POST /oauth2/revoke` — RFC 7009.
- `GET  /oauth2/.well-known/jwks.json` — JWKS for ID-token signing keys.

There is **no** registration endpoint.

**Resources consumed:**

| Endpoint                                  | Reads                                                 | Writes                                                                                            |
| ----------------------------------------- | ----------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `/.well-known/oauth-authorization-server` | —                                                     | —                                                                                                 |
| `/.well-known/openid-configuration`       | —                                                     | —                                                                                                 |
| `/authorize`                              | `users` (oauth2 currentUser)                          | `auth_codes` (mode=`oauth2`, records inbound `client_id` + optional `code_challenge` + `scope`)   |
| `/token`                                  | `auth_codes` (validates `code_verifier` when present) | `tokens` (mode=`oauth2`, kinds=`access_token` + `refresh_token` [+ `id_token` if `openid` scope]) |
| `/userinfo`                               | `tokens`, `users`                                     | —                                                                                                 |
| `/revoke`                                 | `tokens`                                              | `tokens.revoked_at`                                                                               |
| `/.well-known/jwks.json`                  | —                                                     | —                                                                                                 |

### 7.5 Shared protocol behaviours

- All modes resolve user identity from the **per-mode** `currentUser`
  (§3) at any point a real IDP would render a login screen, and
  immediately redirect with the issued code. **No mode ever renders an
  interactive surface.**
- All modes accept any inbound `client_id` / `client_secret` / `redirect_uri`
  without validation (§5.2). Identity resolution is the only check.
- Tokens issued by every mode are recorded in the `tokens` table so a test
  can introspect the full set of tokens that have been issued in a run.

---

## 8. CLI surface

```
gram dev-idp [flags]
```

Flags:

**Every flag has a backing env var** (matches the `start` / `worker` / `admin`
convention in `server/cmd/gram/`). Where an existing project-wide env var
already covers the concept (`SPEAKEASY_SECRET_KEY`, `WORKOS_API_KEY`,
`GRAM_CONFIG_FILE`) we reuse it rather than minting a new one — that way
`madprocs` and the existing `mise.local.toml` can configure dev-idp
without per-binary special-casing. **`GRAM_JWT_SIGNING_KEY` is
deliberately not reused** — that key is HS256 (symmetric) and can't back
a JWKS surface; dev-idp uses its own RSA keypair instead (§5.3, §10).

| Flag                     | Env var                       | Default                  | Notes                                                                                                                                                                                                                                                                                         |
| ------------------------ | ----------------------------- | ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--address`              | `GRAM_DEVIDP_ADDRESS`         | `:35291`                 | `host:port` listen string. Bare `:35291` binds all interfaces; `127.0.0.1:35291` binds loopback. Default port matches today's `mock-speakeasy-idp`.                                                                                                                                           |
| `--control-address`      | `GRAM_DEVIDP_CONTROL_ADDRESS` | `:35292`                 | health / pprof / control listener.                                                                                                                                                                                                                                                            |
| `--external-url`         | `GRAM_DEVIDP_EXTERNAL_URL`    | derived from address     | Public base URL the modes embed in discovery docs and redirect URIs. Set this when dev-idp is reachable through ngrok / a reverse proxy / a worktree-local hostname.                                                                                                                          |
| `--database-url`         | `GRAM_DEVIDP_DATABASE_URL`    | required                 | dev-idp's **own** Postgres database. Atlas declarative apply will reshape it to match SDL — never point this at production.                                                                                                                                                                   |
| `--speakeasy-secret-key` | `SPEAKEASY_SECRET_KEY`        | `test-secret`            | The legacy `mock-speakeasy` header secret. Reuses the existing env var so the `start` / `worker` procs can continue to share their value.                                                                                                                                                     |
| `--rsa-private-key`      | `GRAM_DEVIDP_RSA_PRIVATE_KEY` | ephemeral per boot       | PEM-encoded RSA private key — the **only** signing key dev-idp uses (§5.3). Signs OIDC `id_token`s; public half published via JWKS. When omitted, dev-idp generates a fresh keypair on boot. dev-idp does **not** consume `GRAM_JWT_SIGNING_KEY` (that key is HS256, incompatible with JWKS). |
| `--workos-api-key`       | `WORKOS_API_KEY`              | (none)                   | When set, mounts the `/workos/` mode (§7.2). Unset → `/workos/` is not mounted; the other three modes are unaffected. Reuses the existing project env var.                                                                                                                                    |
| `--workos-host`          | `WORKOS_HOST`                 | `https://api.workos.com` | Base URL of the WorkOS API. Override for staging / sandbox / a recorded fixture host.                                                                                                                                                                                                         |
| standard `--with-otel-*` |                               | as elsewhere             | matches start/worker/admin.                                                                                                                                                                                                                                                                   |
| standard `--config-file` | `GRAM_CONFIG_FILE`            | as elsewhere             | matches start/worker/admin — supports JSON / TOML / YAML for setting any of the above.                                                                                                                                                                                                        |

There is **no** `--seed-file` flag. Seeding happens through the management
API (§6.1): a test or onboarding script POSTs to `/rpc/users.create`,
`/rpc/organizations.create`, `/rpc/memberships.create` in whatever order
it needs. Keeping the seed surface as code rather than a config file
avoids a second seed format we'd have to keep in lock-step.

The dev-idp binary is registered alongside `start`, `worker`, `admin`,
`version` in `server/cmd/gram/root.go`.

---

## 9. Process for cutting over from `mock-speakeasy-idp`

Sequence (each step its own commit):

1. Land `server/internal/devidp/` skeleton with the Postgres store, the
   organizations / users / memberships management API, and the
   `mock-speakeasy` mode behaving identically to today's
   `mock-speakeasy-idp`. Verify by pointing local Gram at `gram dev-idp`
   instead of the standalone binary.
2. Land `oauth2-1` + `oauth2` modes plus the `devIdp.*` dev RPC.
3. Land `workos` mode (real WorkOS API proxy, §7.2).
4. Wire integration tests onto `gram dev-idp`. `dev-idp-dashboard` ships in
   parallel (separate top-level project, not embedded).
5. Delete `mock-speakeasy-idp/` once no caller references it.

Steps 1 and 5 each become Cleanup tickets in `project.md`.

---

## 10. Out of scope

- Persisted asymmetric key material. ID tokens are signed with RS256
  (§5.3) but the keypair is **ephemeral** — generated fresh on every
  dev-idp boot. Tests that want stable JWKS across restarts can pass a
  `--rsa-private-key` PEM at boot.
- Federation between modes (i.e. logging in via `/workos/` should leave
  `/oauth2-1/` unauthenticated). Each mode keeps its own token bag and
  its own currentUser.
- Any UX that isn't operator-visibility — the `dev-idp-dashboard`'s full
  design is its own doc, but it never renders anything end-user-facing
  (auth is non-interactive, §3).
- Persistent consent records. There is no consent flow to persist (§3).
- Client authentication of any kind. All `client_id` / `client_secret` /
  `redirect_uri` values are accepted as-is (§5.2).

---

## 11. Open questions

_None outstanding._ Every open question has a decision; see the resolved
list below.

**Resolved (kept for paper trail):**

- ~~SQLite vs Postgres.~~ Postgres, separate database, declarative apply.
- ~~Single file vs sub-package for the cmd entrypoint.~~ Single file
  `server/cmd/gram/dev-idp.go`, sibling of `admin.go`.
- ~~`mock-idp-ui` build hand-off.~~ Replaced by `dev-idp-dashboard`, a
  separate top-level project; not embedded.
- ~~Goa or plain HTTP for protocol surfaces.~~ Plain HTTP for protocol
  modes; Goa for the management API.
- ~~Per-request `X-Dev-Idp-As` header.~~ Dropped — risk of leaking via
  outbound `doHTTP` not worth it.
- ~~`consents` resource.~~ Not needed. Auth is non-interactive (§3); no
  consent flow exists to persist.
- ~~`/rpc/applications.*` CRUD.~~ Not needed. There is no `applications`
  table at all — register is stateless and clients are unauthenticated
  (§5.2).
- ~~`auto_consent` toggle.~~ Removed. Auth is always non-interactive (§3).
- ~~Single global `currentUser`.~~ Replaced by **per-mode**
  currentUser (§3) so MCP traffic and management-API traffic in the same
  Gram process can belong to different users without contradiction.
- ~~Stateful client registration.~~ `oauth2-1/register` is stateless;
  every mode accepts every `client_id` / `client_secret` (§5.2).
- ~~`oauth2dot1` naming.~~ Renamed to `oauth2-1` everywhere.
- ~~Goa design + gen at top-level (`server/design/`, `server/gen/`).~~
  Both nested inside `server/internal/devidp/` (§4 layout, §6.3) — keeps
  dev-idp encapsulated and trivially deletable.
- ~~In-memory currentUser.~~ Persisted in the `current_users`
  Postgres table (§3, §5) — survives dev-idp restarts. Wipe by going
  through Postgres directly or running `mise db:devidp:reset` (which
  drops the whole schema).
- ~~`/oauth2-1/` and `/oauth2/` are not OIDC-compliant.~~ Both modes now
  serve `/.well-known/openid-configuration`, a `/userinfo` endpoint, and
  a JWKS document; ID tokens are issued when `scope=openid` is requested
  (§7.3, §7.4). Future-proofs both modes for OIDC-aware tests without
  needing a fifth mode.
- ~~Mixed HS256 + RS256 signing.~~ dev-idp uses **only** an RSA keypair
  for any JWT it signs (just OIDC `id_token`s, today). Access tokens,
  refresh tokens, and mock-speakeasy id tokens are **opaque random
  bytes** — not JWTs, not signed (§5.3, §10). `GRAM_JWT_SIGNING_KEY` is
  not consumed by dev-idp at all; that key is HS256 and incompatible
  with JWKS.
- ~~OIDC for the `workos` mode.~~ Replaced with a thin REST proxy over
  the real WorkOS API, configured by `--workos-api-key` (§7.2). OIDC
  added round-trips without exercising any code path the dev-idp is here
  to support.
- ~~No PKCE on `/oauth2/`.~~ PKCE is now supported but optional on
  `oauth2` (§7.4) so we exercise upstream PKCE handling against a 2.0
  issuer too. `oauth2-1` keeps PKCE-required.
- ~~`--seed-file` flag.~~ Dropped. Seeding goes through the management
  API (§6.1) so there is one seed format, not two.
- ~~Implicit / undocumented config.~~ Every CLI flag has an explicit
  backing env var (§8); existing project-wide env vars
  (`SPEAKEASY_SECRET_KEY`, `WORKOS_API_KEY`, `GRAM_CONFIG_FILE`) are
  reused so dev-idp slots into existing `mise.local.toml` / `madprocs`
  setups without per-binary plumbing.
- ~~Atlas-apply at dev-idp startup.~~ Not done. The binary trusts the
  schema is already applied via `mise db:devidp:apply` and errors
  normally on schema mismatch (§4 layout, §5.4). No shelling out.
- ~~Auth on the management API.~~ Permanently unauthenticated. dev-idp
  is a localhost-only tool; if it's ever reachable from elsewhere
  something has gone wrong. No boot-token header.
- ~~Skipping `workos` mode in the first cut.~~ Not skipped — the
  `workos` mode is in the first cut and uniquely **does not depend on
  the dev-idp DB** for identity. It proxies the real WorkOS API and
  keys its currentUser by WorkOS `sub` (§7.2). Its dashboard surface is
  separate from the local-mode panes.
