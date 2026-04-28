# WorkOS Role Sync Production Readiness

## Context

Gram is adding local database state synchronized from WorkOS so a later PR can
move RBAC role membership resolution out of the request path.

The desired production model is:

1. WorkOS webhook arrives.
2. The webhook payload is treated only as a trigger.
3. A Temporal workflow queries WorkOS for the latest reality since the stored
   cursor.
4. Gram upserts local snapshots of organizations, roles, memberships, and role
   assignments.
5. Every authenticated request resolves grants using only local database reads.

The important eventual request-path invariant is that role grants are loaded on
every request. That path should be fast and DB-local. The current PR does not
need to switch request handling away from WorkOS; that migration is a follow-up.

## Current Work Reviewed

The `workos-sync` branch adds:

- WorkOS webhook endpoint: `/rpc/external.receiveWorkOSWebhook`
- Temporal workflow for per-organization WorkOS event processing
- Periodic reconciliation schedule
- `workos_organization_syncs` cursor table
- `organization_roles` local role table
- `organization_user_roles` local user-to-role join table
- event handlers for organization, membership, and role events

The work is a useful skeleton. For this PR, the review should focus on whether
the synced tables can safely support the later request-path migration and
whether sync progress can be tracked reliably.

## Decisions

### WorkOS Leaves The Request Path In A Later PR

The final architecture should resolve the user's grants from Postgres only.

Do not call `GetOrgMembership`, `ListMembers`, `ListRoles`, or any other WorkOS
API from RBAC request preparation. WorkOS is an external source of truth for sync
workers, not an authorization dependency at request time.

Redis can still be used as an optimization later, but the database must remain
the authoritative request-path source.

This is not a blocker for the current PR if the current PR only introduces sync
state. It is a blocker before the synced role tables are used for authorization.

### Role Grants Use Local Role IDs

Principal grants for roles should use the local `organization_roles.id`.

Use:

```text
role:<organization_roles.id>
```

Do not use:

```text
role:<workos_slug>
```

Role slugs are user-facing WorkOS metadata. They are not a durable authorization
identifier. Storing grants by local role ID avoids drift if a slug changes or a
role is deleted and recreated.

### Store WorkOS Role IDs

`organization_roles` should store WorkOS's role ID in addition to slug/name
metadata.

Recommended shape:

```sql
CREATE TABLE organization_roles (
  id UUID NOT NULL DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  workos_id TEXT NOT NULL,
  workos_slug TEXT NOT NULL,
  workos_name TEXT NOT NULL,
  workos_description TEXT,
  workos_created_at timestamptz NOT NULL,
  workos_updated_at timestamptz NOT NULL,
  workos_deleted_at timestamptz,
  workos_deleted boolean NOT NULL GENERATED ALWAYS AS (workos_deleted_at IS NOT NULL) STORED,
  workos_last_event_id TEXT,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  CONSTRAINT organization_roles_pkey PRIMARY KEY (id),
  CONSTRAINT organization_roles_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organization_metadata (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX organization_roles_organization_id_workos_id_key
ON organization_roles (organization_id, workos_id);

CREATE UNIQUE INDEX organization_roles_organization_id_workos_slug_key
ON organization_roles (organization_id, workos_slug)
WHERE deleted IS FALSE AND workos_deleted IS FALSE;
```

### User Role Assignments Are Local Join Rows

`organization_user_roles` is the correct place to store the local snapshot of a
user's WorkOS role assignments.

Recommended constraints:

```sql
CREATE TABLE organization_user_roles (
  id UUID NOT NULL DEFAULT generate_uuidv7(),
  organization_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  role_id UUID NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT organization_user_roles_pkey PRIMARY KEY (id),
  CONSTRAINT organization_user_roles_organization_id_user_id_role_id_key
    UNIQUE (organization_id, user_id, role_id),
  CONSTRAINT organization_user_roles_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organization_metadata (id) ON DELETE CASCADE,
  CONSTRAINT organization_user_roles_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT organization_user_roles_role_id_fkey
    FOREIGN KEY (role_id) REFERENCES organization_roles (id) ON DELETE CASCADE
);

CREATE INDEX organization_user_roles_organization_id_user_id_idx
ON organization_user_roles (organization_id, user_id);
```

Avoid `ON DELETE SET NULL` on non-null columns. This is a join table; cascading
delete is the clearer behavior.

### Hot Grant Load Query

The request path should use one DB query to resolve both direct user grants and
role grants.

Target query shape:

```sql
WITH principals AS (
  SELECT 'user:' || @user_id AS principal_urn
  UNION ALL
  SELECT 'role:' || role_id::text
  FROM organization_user_roles
  WHERE organization_id = @organization_id
    AND user_id = @user_id
)
SELECT scope, resource
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn IN (SELECT principal_urn FROM principals);
```

The existing `principal_grants` index on
`(organization_id, principal_urn, scope, resource)` is appropriate for this
lookup.

### Sync Should Be Snapshot-Oriented

The webhook event body should not be treated as authoritative state.

The event should trigger a worker that fetches current WorkOS state and
declaratively upserts local rows. This avoids correctness issues when events
arrive out of order or when Gram has missed a previous event.

At minimum, membership sync must not silently drop role assignments when the
role row has not been synced yet.

### Cursors Track Sync Progress

`workos_organization_syncs` should track the cursor per WorkOS organization.

Recommended additions:

- `updated_at timestamptz NOT NULL DEFAULT clock_timestamp()`
- optional `last_synced_at timestamptz`
- optional `last_error_at timestamptz`
- optional `last_error TEXT`

`created_at` should not be overwritten when the cursor advances.

### Advisory Locks Are Not Needed

Temporal workflow identity and `WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING` are
enough to serialize work per WorkOS organization.

The advisory lock helper and generated advisory lock queries can be removed
unless a separate DB-only synchronization path is introduced.

## Known Problems And Scope

- RBAC request preparation still resolves role slug via WorkOS. This is accepted
  as out of scope for the current PR.
- Role grants are still keyed by role slug. This can remain temporarily only if
  the current PR does not attempt to make synced DB roles authoritative for
  authorization.
- `organization_roles` does not store WorkOS role ID.
- Membership sync resolves slugs only if role rows already exist.
- A membership event before a role event can delete/omit local role assignments
  and still advance the cursor.
- Webhook enqueue failures are logged but still return success to WorkOS.
- `workos_user_syncs` has no clear owner or key and appears premature.
- Existing tests validate membership relationship linking, but do not assert
  `organization_user_roles` correctness.

## Production Checklist

### Current PR

- [ ] Add `workos_id` to `organization_roles`.
- [ ] Make membership sync robust when role events have not arrived yet.
- [ ] Prefer snapshot reconciliation for roles and memberships.
- [ ] Return non-2xx from the webhook endpoint when Temporal enqueue fails.
- [ ] Add `updated_at` or `last_synced_at` to WorkOS sync cursor tables.
- [ ] Remove unused advisory lock code.
- [ ] Remove or fully design `workos_user_syncs`.
- [ ] Add tests for role create/update/delete events.
- [ ] Add tests for membership role assignment into `organization_user_roles`.
- [ ] Add tests for membership event before role event.
- [ ] Add tests for slug rename without grant loss.
- [ ] Add tests for cursor advancement only after successful local writes.
- [ ] Run `mise run gen:sqlc-server` after query/schema updates.
- [ ] Run `mise lint:server`.
- [ ] Run server tests covering `access`, `organizations`, and WorkOS sync
      activities.
- [ ] Run migration on a prod-like database copy.

### Follow-Up Request-Path PR

- [ ] Change role grant principals from `role:<slug>` to `role:<role_id>`.
- [ ] Add a migration/backfill path for existing role grants.
- [ ] Replace request-path WorkOS role resolution with local DB role assignment
      lookup.
- [ ] Add a single SQLc query that returns effective grants for
      user + assigned role IDs.
- [ ] Remove role slug cache from the authorization hot path, or make it an
      optional optimization only after DB correctness is in place.
- [ ] Ensure role deletion removes role assignments and role grants by local
      role ID.
- [ ] Capture `EXPLAIN ANALYZE` for the effective-grants request-path query.

## Rollout Plan

1. Land schema in an expand-compatible migration.
2. Backfill `organization_roles.workos_id` and role grant principals.
3. Ship sync worker changes while the request path still uses the current WorkOS
   resolution behavior.
4. Enable webhook ingestion and reconciliation in staging.
5. Verify cursor progress, workflow failures, and synced role/member snapshots.
6. Enable production reconciliation first.
7. Enable production webhook ingestion.
8. In a follow-up PR, switch request-path RBAC to DB-local role IDs.
9. Monitor WorkOS workflow failures, sync lag, grant-load latency, and forbidden
   response rate.
