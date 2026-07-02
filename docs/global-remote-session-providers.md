# Global Remote Session Providers: Handover

Status: **backend + migration + SDK + dashboard wiring DONE and green.**
Last updated: 2026-06-30. Author: Walker + Claude.

All work is **uncommitted on `main`** in the working tree. Nothing committed yet.

## Goal (unchanged)

Let **Speakeasy employees** curate a **platform-wide catalog** of remote session providers
(HubSpot, Google Workspace) that every customer org can eventually use. A "provider" is a Remote
Session **Issuer** (RSI, the upstream OAuth provider) paired with one or more Remote Session
**Clients** (RSC, Speakeasy's registered OAuth app credentials).

"Global" = **`project_id IS NULL` AND `organization_id IS NULL`**, shared across all orgs. Distinct
from the existing "org-level" issuer (`project_id NULL`, `organization_id` set) which is already
shipped.

Scope of THIS unit of work: **creation APIs + dashboard wiring only.** Consumption (projects
inheriting/using globals) is deferred — rows will exist that nothing reads yet. Intentional.

## What is DONE (verified: `mise build:server`, `mise lint:server`, `mise lint:migrations` clean;

`mise test:server ./internal/remotesessions/...` = 183 pass, incl. 8 new)

### 1. Goa service `adminRemoteSessions` (core server, port 8080)

- **Design:** `server/design/platformadmin/remotesessions/design.go` (NEW, first nested design pkg;
  package name `remotesessions`). `Service("adminRemoteSessions")`, `Security(security.Session)`
  only (dashboard-only, no ByKey). Routes `/rpc/adminRemoteSessions.*`. 10 methods:
  `createGlobalIssuer`, `listGlobalIssuers`, `getGlobalIssuer`, `updateGlobalIssuer`,
  `deleteGlobalIssuer`, `createGlobalClient`, `listGlobalClients` (by `remote_session_issuer_id`),
  `getGlobalClient`, `updateGlobalClient`, `deleteGlobalClient`.
  - Issuer create reuses `Extend(rsissuers.CreateRemoteSessionIssuerForm)`; issuer update
    `Extend(rsissuers.UpdateRemoteSessionIssuerForm)`; client update
    `Extend(rsclients.UpdateRemoteSessionClientForm)`. Client create uses a bespoke
    `CreateGlobalRemoteSessionClientForm` (defined at the bottom of the design file) — same as the
    project client form **minus `user_session_issuer_ids`** (globals have no USI attachments).
  - List results reuse `rsissuers.ListRemoteSessionIssuersResult` /
    `rsclients.ListRemoteSessionClientsResult`.
- Blank import added to `server/design/gram.go` (alphabetised, after `packages`).
- **Generated** (via `mise run gen:goa-server`): `server/gen/admin_remote_sessions/` (pkg
  `adminremotesessions`), `server/gen/http/admin_remote_sessions/`. Payload structs in
  `server/gen/admin_remote_sessions/service.go`.

### 2. Impl on the existing `*remotesessions.Service`

- **`server/internal/remotesessions/adminhandlers.go`** (NEW). 10 handlers. Each starts with
  `s.requireGlobalAdmin(ctx)` → returns `(authCtx, logger, err)`; gates inline on `authCtx.IsAdmin`
  (403 `oops.CodeForbidden` "platform admin required"). **No `authz.Require`** (globals have no
  project/org to scope a grant). Mutations: tx + structured-log audit via `logGlobalMutation(...)`
  (no `auditlogs` rows — `audit_log.organization_id` is `NOT NULL`). Secret encryption via `s.enc`,
  blank-on-update keeps current. `deleteGlobalIssuer` blocked when live clients reference it (409);
  `deleteGlobalClient` cascades `SoftDeleteRemoteSessionsByClientID`.
  - Helper `orEmptySlice` coalesces nil `*_supported` arrays → `[]` on issuer create (the columns
    are `NOT NULL`; an explicit NULL in the INSERT bypasses the column default). **Delta from
    handover decision #8, but consistent with it** ("omitted optionals default server-side").
- Mounted in the existing `remotesessions.Attach` (`impl.go`) alongside the other 4 services; added
  `_ adminrsgen.Service` / `_ adminrsgen.Auther` compile-time assertions. `start.go` already calls
  `remotesessions.Attach`, so no new wiring there.
- **`server/internal/remotesessions/errors.go`**: added `isGlobalRemoteSessionIssuerSlugConflict`
  (checks constraint `remote_session_issuers_global_slug_key`). Used for 409 on create/update.

### 3. SQLc (`server/internal/remotesessions/queries.sql`, regen'd via `mise run gen:sqlc-server`)

8 new global-scoped queries, all `WHERE ... project_id IS NULL AND organization_id IS NULL AND
deleted IS FALSE`: `ListGlobalRemoteSessionIssuers`, `GetGlobalRemoteSessionIssuerByID`,
`UpdateGlobalRemoteSessionIssuer`, `DeleteGlobalRemoteSessionIssuer`,
`ListGlobalRemoteSessionClientsByIssuerID`, `GetGlobalRemoteSessionClientByID`,
`UpdateGlobalRemoteSessionClient`, `DeleteGlobalRemoteSessionClient`. Create paths **reuse**
`CreateRemoteSessionIssuer` / `CreateRemoteSessionClient` with NULL project_id + NULL org_id.

### 4. Model views (`server/internal/mv/remotesessionclient.go`)

Added **`BuildGlobalRemoteSessionClientView`** — renders project_id "" (empty) for null instead of
erroring like the project-scoped `BuildRemoteSessionClientView` does (which treats null project_id
as an invariant violation). Returns empty `UserSessionIssuerIds`.

### 5. Design type relaxation (compatible)

`server/design/remotesessionclients/design.go`: `RemoteSessionClient.project_id` **dropped
`Format(FormatUUID)`** (still Required) so globals serialize it empty — mirrors the existing
`RemoteSessionIssuer.project_id` precedent. Loosening only; project/org clients still send a valid
UUID.

### 6. Attr builders (`server/internal/attr/conventions.go`)

Added keys + `Slog*` builders: `AuditAction`/`SlogAuditAction`, `AuditSubject`/`SlogAuditSubject`,
`AuditSubjectID`/`SlogAuditSubjectID` (glint `enforceo11yconventions` forbids direct `slog.String`).

### 7. Migration (MUST SHIP IN ITS OWN PR — per `postgresql` skill)

- `server/database/schema.sql`: added partial unique index `remote_session_issuers_global_slug_key`
  on `(slug) WHERE deleted IS FALSE AND project_id IS NULL AND organization_id IS NULL`.
- Generated `server/migrations/20260630213745_remote-session-issuers-global-slug-key.sql`
  (`CONCURRENTLY`, `txmode none`) + `atlas.sum` bump, via `mise run db:diff`. Applied locally OK.

### 8. SDK (regen'd via `mise run gen:sdk`)

`@gram/client` now has the 10 funcs (`adminRemoteSessionsCreateGlobalIssuer` …) + react-query hooks

- models. Also touched `.speakeasy/out.openapi.yaml`, `server/gen/http/openapi3.{json,yaml}`,
  `server/gen/http/cli/gram/cli.go`.

### 8 new tests: `server/internal/remotesessions/adminhandlers_test.go`

Create success (project_id/org empty in view), requires-admin (403), slug conflict (409), list+get,
get not-found (404), update, full client lifecycle (create client → issuer-delete blocked 409 →
delete client → delete issuer), create-client-rejects-non-global-issuer (404). Uses the existing
`withAdmin(t, ctx)` helper (default test ctx is non-admin).

## Dashboard wiring (DONE: `pnpm -F dashboard type-check` + `lint` green)

`client/dashboard/src/components/global-remote-session-clients.tsx` (`GlobalRSCsModal`) now uses the
generated react-query hooks. Removed `SEED_ENTRIES` + the paired `GlobalEntry`; issuer and client
modeled separately. Left pane = `useGlobalRemoteSessionIssuers()` (gated `enabled: open`),
client-side search; selecting an issuer loads its clients via `useGlobalRemoteSessionClients`. Draft
re-derived via a `useEffect` keyed on `(selectedIssuerId, primaryClient?.id)` so async client load
fills the client fields. Create (1:1) = `createIssuer.mutateAsync` then `createGlobalClient` with the
returned id; on client-create failure the orphan issuer stays selected with a toast prompt to add a
client or delete it. Edit = `updateIssuer` + (`updateClient` when a client exists — **Client ID
disabled, immutable**; the update form has no `client_id` — else `createClient` for an orphan).
Delete = delete all clients then the issuer. Real `RemoteSessionClient` has **no** `hasSecret` /
`secretSetAt` (dropped from the old mock); `createdAt`/`clientIdIssuedAt` are `Date`. Multi-client
issuers: edit the first, list the rest read-only ("Other clients"). Toasts via `sonner`,
`invalidateAllGlobalRemoteSessionIssuers` + `invalidateAllGlobalRemoteSessionClients` after every
mutation. The two sibling files (`platform-admin-toolbar.tsx`, `platform-admin-panel.tsx`) mount the
modal and needed no change.

### Original handover notes (for reference)

The mock modeled a provider as one paired `GlobalEntry` (issuer + exactly one client, 1:1) seeded
with `SEED_ENTRIES` (HubSpot, Google Workspace) in `useState`.

**Locked UX (handover decision):** keep the 1:1 create flow; issuer detail **reads all** clients
(display if >1, don't hide data). Remove `SEED_ENTRIES` + the paired `GlobalEntry` type; model
issuer and client separately.

**Hooks/shapes (from `@gram/client`, already verified in the regen'd SDK):**

- List issuers (left pane): `useGlobalRemoteSessionIssuers()` → `{ data?: { items, nextCursor } }`.
  Invalidate with `invalidateGlobalRemoteSessionIssuers(queryClient)`.
- List a selected issuer's clients: `useGlobalRemoteSessionClients({ remoteSessionIssuerId })` →
  `{ data?: { items, nextCursor } }`. Invalidate `invalidateAllGlobalRemoteSessionClients(qc)`.
- Mutations (all return `UseMutationResult`, call `.mutateAsync({ request: {...} })`):
  - `useCreateGlobalRemoteSessionIssuerMutation()` → `{ request: { createRemoteSessionIssuerForm: { slug, issuer, name?, authorizationEndpoint?, tokenEndpoint?, registrationEndpoint?, jwksUri?, scopesSupported?, oidc?, passthrough?, ... } } }`
  - `useUpdateGlobalRemoteSessionIssuerMutation()` → `{ request: { updateRemoteSessionIssuerForm: { id, slug?, issuer?, ... } } }`
  - `useDeleteGlobalRemoteSessionIssuerMutation()` → `{ request: { id } }`
  - `useCreateGlobalRemoteSessionClientMutation()` → `{ request: { createGlobalRemoteSessionClientForm: { remoteSessionIssuerId, clientId, clientSecret?, tokenEndpointAuthMethod?, scope?, audience? } } }`
  - `useUpdateGlobalRemoteSessionClientMutation()` → `{ request: { updateRemoteSessionClientForm: { id, clientSecret?, tokenEndpointAuthMethod?, scope?, audience? } } }`
  - `useDeleteGlobalRemoteSessionClientMutation()` → `{ request: { id } }`

**Wiring plan:**

1. Left pane = `useGlobalRemoteSessionIssuers()` items (keep the existing search box, filter client
   side). Selecting an issuer fetches its clients via `useGlobalRemoteSessionClients`.
2. "New provider" (1:1): `createGlobalIssuer.mutateAsync` then, with the returned issuer id,
   `createGlobalClient.mutateAsync`. On client-create failure surface the error and allow cleanup of
   the orphan issuer (or leave it for the operator to add a client / delete). Invalidate issuers +
   clients after.
3. Edit: `updateGlobalIssuer` for the issuer fields; for the (first) client, `updateGlobalClient`
   (blank secret = keep). If the issuer has >1 client, render them read-only-ish / pick the first to
   edit — don't hide the rest.
4. Delete: delete the client(s) first (`deleteGlobalClient`), then `deleteGlobalIssuer` (issuer
   delete 409s while a live client references it — already enforced server-side).
5. Use react-query patterns + toasts per the `frontend` skill. Invalidate ALL relevant query keys.
6. Verify: `pnpm -F dashboard type-check` and `pnpm -F dashboard lint`.

Mock already has all the form fields (issuer: name/slug/issuer/auth+token+registration endpoints/
jwks/scopes/oidc/passthrough; client: clientId/secret/tokenEndpointAuthMethod/scope/audience) and
the auth-method `Select` wired to `CreateRemoteSessionClientFormTokenEndpointAuthMethod` enum —
reuse that. `parseList` (comma→array) and `formatDate` helpers already there.

## PR status

- **Migration PR: OPENED** → https://github.com/speakeasy-api/gram/pull/3803 (branch
  `walker/mig-remote-session-issuers-global-slug-key`). Exactly 3 files: `schema.sql` (index only),
  the generated migration, `atlas.sum`. No changeset (migration-only PRs don't use them). Local
  `atlas migrate lint` snapshot step errored ("connected database is not clean") — env-only, CI runs
  it against a clean DB; `txmode none` + ordering checks passed.
- Backend + SDK PR: not yet cut.
- Dashboard PR: not yet cut.

## PR split (important)

Per the `postgresql` skill, the **migration ships in its own PR** (no business logic alongside):
just `server/database/schema.sql` + `server/migrations/2026...global-slug-key.sql` +
`server/migrations/atlas.sum`. Backend (design/gen/impl/queries/mv/attr/errors/tests) + SDK regen =
a second PR (`"server": minor` changeset). Dashboard wiring = third PR (or folded with backend).
Add `.changeset/*.md` files when committing (not done yet).

## Deferred (NOT in this work)

- **Consumption path** (big follow-up): runtime token-minting reads
  (`GetRemoteSessionIssuerByID`, `ListRemoteSessionIssuersByProjectID`) must add
  `OR (project_id IS NULL AND organization_id IS NULL)`; slug precedence project > org > global;
  global-client project fallback in `resolveOrganizationClientProject`. Hot path; flag-gate it.
- `createGlobalCimdClient` (CIMD), `discoverGlobalIssuer` (RFC 8414 autofill), real `auditlogs`.

## Key reference anchors

- Inline platform-admin gate precedent: `clienthandlers.go` `CloneClientFromOAuthProxyProvider`
  (`if !authCtx.IsAdmin { 403 }`). `AuthContext.IsAdmin`:
  `server/internal/contextvalues/context.go:20`. (`authCtx.Email` is `*string`.)
- Org-scoped admin service modeled on: `server/internal/remotesessions/organizationhandlers.go`.
- Generated payloads: `server/gen/admin_remote_sessions/service.go`.
- Gen tasks: `mise run gen:goa-server`, `mise run gen:sqlc-server` (needs `mise run infra:start`),
  `mise run gen:sdk`, `mise run db:diff <name>`.
