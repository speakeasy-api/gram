# WorkOS Org Sync — Implementation Plan

## Current State

Login-time pull sync exists (`syncWorkOSMemberships` in `server/internal/auth/sessions/speakeasyconnections.go`):

- On auth, resolves WorkOS user ID by email, calls `ListUserMemberships`, declaratively upserts `organization_user_relationships`
- Idempotent DB primitives exist: `SetUserWorkosID`, `SetOrgWorkosID`, `SetUserWorkOSMemberships`
- WorkOS client has full membership/invite/role API coverage

---

## Phase 1 — Webhook Ingestion + DB Cursor ✅ DONE

**Goal:** receive WorkOS events via webhook, enqueue per-org Temporal workflow, persist event cursor per org.

### Completed

- `workos_organization_syncs` table: one row per org, unique index on `workos_organization_id`, upsert semantics
- Webhook HTTP endpoint: `POST /internal/webhooks/workos`, HMAC-verified, returns error on enqueue failure
- `GetOrganizationSyncLastEventID`: reads cursor, returns empty string on no row
- `SetOrganizationSyncLastEventID`: upserts cursor after each processed event

---

## Phase 2 — Org Identity Mapping ✅ DONE

**Goal:** correctly link WorkOS orgs to Gram orgs without duplicates.

### Completed

- On org create/update: first check if already linked via `GetOrganizationIDByWorkosID`
- If not yet linked: require non-empty `external_id` (error if missing)
- Use `external_id` directly as org ID — `UpsertOrganizationMetadataFromWorkOS` creates or updates
- Orgs can be created from WorkOS events when `external_id` is present

---

## Phase 3 — Event Processing Workflow ✅ DONE

**Goal:** process all events for an org since last cursor, advance cursor monotonically.

### Completed

- Cursor loaded from DB at start of activity (or from `params.SinceEventID` if overridden)
- One page of 100 events per activity execution
- Cursor advanced after each event within transaction
- `HasMore: len(page) == pageSize` — if full page returned, workflow re-triggers via debounce signal
- Workflow identity dedup (`WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING`) handles concurrent webhooks

---

## Phase 4 — Backfill / Reconciliation Schedule

**Goal:** catch missed events from downtime, cursor bugs, or webhook gaps.

### Tasks

1. **Re-enable periodic reconciliation schedule** (currently commented out in `process_workos_org_events.go`)
   - Every 30 min (already in commented code): iterate all linked orgs, enqueue sync workflow for each
   - Reuses same workflow — cursor guarantees only new events are processed
   - Need a query: `ListOrganizationsWithWorkosID` to drive the schedule

2. **Add `ListOrganizationsWithWorkosID` query** in `server/internal/organizations/queries.sql`

3. **Implement schedule startup** in `server/cmd/gram/worker.go` or `start.go`

---

## Phase 5 — Tests

**Priority tests before shipping:**

- Cursor resumes from stored event ID (first page starts at cursor)
- Multiple processed pages advance cursor monotonically
- `external_id` missing → error returned (not skipped)
- Existing Gram org matched via `external_id` → linked, not duplicated
- Already-linked org → idempotent
- Membership event for unknown org → warn + skip
- Membership event for unknown user → warn + skip
- Replayed/out-of-order events → idempotent

---

## Decisions

1. **`external_id` = Gram org UUID** — always set by Speakeasy. If empty → return error (not skip). This is a bug, not a graceful edge case.
2. **`external_id` = Gram org ID** — used directly as `organization_id`. `UpsertOrganizationMetadataFromWorkOS` creates the row if it doesn't exist.
3. **User sync out of scope** — login-time `syncWorkOSMemberships` handles the user→org path.
4. **Unknown user in membership event → warn + skip.** Login-time sync and Phase 4 reconciliation are the safety nets.
5. **No advisory locks** — Temporal workflow identity dedup (`WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING`) is sufficient for serialization.
6. **Webhook endpoint** — raw HTTP handler registered in orgs `Attach()`, same pattern as Polar webhook in `usage/impl.go:74`. HMAC-only auth, no Goa routing.
7. **One page per activity** — 100 events max per run. Full page → `HasMore: true` → debounce re-triggers workflow for next page.

---

## E2E Testing Plan

### Prerequisites

1. Set in `mise.local.toml`:
   ```toml
   WORKOS_API_KEY = "<key from WorkOS dashboard>"
   WORKOS_WEBHOOK_SECRET = "<webhook secret from WorkOS dashboard>"
   ```
2. `mise start:server --dev-single-process`
3. Expose local server: `ngrok http 8080`
4. WorkOS dashboard → Webhooks → register `https://<ngrok>/rpc/external.receiveWorkOSWebhook`
5. Subscribe to: `organization.*`, `organization_membership.*`, `organization_role.*`

---

### Test 1 — Webhook signature validation (:check:)

```bash
# Bad signature → 401
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://localhost:8080/rpc/external.receiveWorkOSWebhook \
  -H "workos-signature: t=bad,v1=bad" \
  -d '{"event":"organization.created","data":{"id":"org_1","object":"organization"}}'
# expect: 400
```

---

### Test 2 — Org created via webhook links to Gram org (:check)

**Setup:** In WorkOS dashboard, set org `external_id` to an existing Gram org UUID:

```sql
SELECT id FROM organization_metadata LIMIT 5;
```

**Trigger:** WorkOS dashboard → Events → send test `organization.created` for that org.

**Verify:**

```sql
SELECT id, workos_id FROM organization_metadata WHERE id = '<gram_org_id>';
SELECT * FROM workos_organization_syncs WHERE workos_organization_id = '<workos_org_id>';
```

---

### Test 3 — Membership event links user (cant check)

**Setup:**

1. Find a user with a WorkOS ID: `SELECT id, workos_id FROM users WHERE workos_id IS NOT NULL LIMIT 1`
2. Ensure that user is a member of the WorkOS org

**Trigger:** Send `organization_membership.created` from WorkOS dashboard.

**Verify:**

```sql
SELECT * FROM organization_user_relationships
WHERE organization_id = '<gram_org_id>' AND user_id = '<gram_user_id>';
```

---

### Test 4 — Cursor advances after events

**Trigger:** Send 2–3 events in sequence for same org.

**Verify:**

```sql
SELECT last_event_id FROM workos_organization_syncs
WHERE workos_organization_id = '<workos_org_id>';
```

Should match most recent event ID in WorkOS dashboard → Events.

---

### Test 5 — Reconciliation schedule fires

In Temporal UI (`http://localhost:8233`):

1. Find schedule `v1:reconcile-workos-organizations-schedule` → trigger manually
2. Verify child workflows appear: `v1:process-workos-org-events:<workos_org_id>`
3. Verify they complete and cursors advance

---

### Test 6 — Rapid webhooks collapse into one run

**Trigger:** Send 5 webhook requests in quick succession for same org.

**Verify in Temporal UI:** Only one workflow execution (or running + one queued signal) for `v1:process-workos-org-events:<workos_org_id>`. Not 5 runs.

---

### Test 7 — Missing `external_id` → error, cursor not advanced

**Trigger:** Remove `external_id` from a WorkOS org, then send `organization.created`.

**Verify:** Temporal workflow fails (visible in UI), cursor in `workos_organization_syncs` not updated.

---

## File Map

| File                                                                    | Purpose                                                                       |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `server/migrations/20260423131203_add-workos-roles-and-sync-tables.sql` | `workos_organization_syncs`, `workos_user_syncs`, `organization_roles` tables |
| `server/internal/thirdparty/workos/queries.sql`                         | cursor load/save queries                                                      |
| `server/internal/background/activities/process_workos_org_events.go`    | activity: cursor load, pagination, event handlers                             |
| `server/internal/background/process_workos_org_events.go`               | Temporal workflow + debounced executor                                        |
| `server/internal/thirdparty/workos/events.go`                           | `ListEvents` client method                                                    |
| `server/internal/external/impl.go`                                      | WorkOS webhook HTTP handler                                                   |

## Leftover work

### Latest context

We have the core work done to listen to workos events and store them in our database. This is working as expected and the data model is mostly finished.

We have a few snags to cover, and some of them related to a specific problem that will be outlined below.

### Problem context 1: eventual consistency

Currently we don't have a proper eventual consistency mechanism in place. One example:

1. We suddenly wipe our database
2. We have a script that repopulates it
3. WorkOS events keep coming in

We may run into a situation where we have a very old workos event ID that changes the already up to date data that we already filled due to our backfill script. So we'll be overwriting data that is already up to date.

#### Solution

For every table that we update in the sync, we need to track the workos_updated_at date (note: not the same as updated_at because we might update tables for other reasons).

The algorithm we need is:

function shouldProcessEvent(row, event) {
if (row.last_event_id == null) {
return event.object.updated_at >= row.workos_updated_at;
}

return event.id > row.last_event_id;
}

### Problem context 2: roles and FK

We've been having the debate around how to handle global / organisation roles and foreign keys. Solution is now clear:

(For context, we're renaming organizaition_user_roles to organization_role_assignments):

1. Add a column role_urn for the role
2. role_urn will be one of `role:global:<gloabl_role_id>` or `role:organization:<organization_role_id>
3. There is no foreign key and no need for it. Any updates have to be handled by the app

### Problem context 3: organisation membership updates

There is a chance that an organisation membership event arrives before we have a local gram user table. This means that we wont have a user ID. We need to assume this can happen and update what we can at the point in time of the event. Eventually that user will get created and have the proper organization membership. What this means:

1. Allow the user ID of a membership to be null (column change).
2. When a membership event comes - if we dont have an internal user ID - we just leave it empty. Eventually we'll be able to fill this and we'll do it when the user does get created

### Backfill script

We need to create a script to backfill the data before the first run (and whenever required). This should look at all the orgs in our DB and query workos to get the current state. Should run as a temporal workflow (manually triggered). This is how global roles get filled originally.

organization_role_assignments
