# WorkOS Sync Paths

This document summarizes the main WorkOS synchronization paths and the resulting local database state.

## Local Tables

The relevant local state is split across:

- `users`: Gram users linked to WorkOS users by `users.workos_id`.
- `organization_user_relationships`: organization membership rows, linked to WorkOS by `workos_user_id` and `workos_membership_id`.
- `organization_role_assignments`: role assignments, linked to WorkOS by `workos_user_id`, `workos_membership_id`, and role URNs.
- WorkOS sync cursor tables: track the last processed WorkOS event IDs for organization and user event streams.

Soft-deleted rows use `deleted_at`. WorkOS events and snapshots are applied only when their WorkOS timestamp/event cursor should win over the local row.

## Organization Membership Event Before User Event

This path happens when an `organization_membership.created` or `organization_membership.updated` event is processed before the matching WorkOS `user.created` or `user.updated` event.

### Known Organization, Unknown User

Input:

- WorkOS organization exists locally.
- WorkOS membership event references a WorkOS user ID.
- No local `users` row exists yet for that WorkOS user ID.

Result:

- `organization_user_relationships` is upserted with:
  - `organization_id`
  - `workos_user_id`
  - `workos_membership_id`
  - `user_id = NULL`
- `organization_role_assignments` is synced with:
  - `workos_user_id`
  - `workos_membership_id`
  - `user_id = NULL`
  - resolved role URNs for the WorkOS role slugs that already exist locally
- The organization event cursor advances.

End state:

- The membership and role assignment are represented locally, but are not yet attached to a Gram user.
- Later user sync can link both tables by matching `workos_user_id`.

### Known Organization, Known User

Input:

- WorkOS organization exists locally.
- Local `users.workos_id` matches the WorkOS membership user ID.

Result:

- `organization_user_relationships` is upserted with `user_id` populated.
- `organization_role_assignments` is synced with `user_id` populated.
- If an older tombstoned relationship exists for the same `(organization_id, user_id)`, the sync reuses/reactivates that relationship instead of creating a conflicting duplicate.

End state:

- The Gram user has an active organization relationship.
- The Gram user has the current role assignments for that WorkOS membership.

### Unknown Organization

Input:

- WorkOS membership event references an organization that has no local Gram organization metadata.

Result:

- The event is skipped.
- No membership or role assignment rows are written.
- The organization event cursor still advances.

End state:

- No local organization state is created from a membership event alone.
- Organization metadata must be linked through the organization sync path.

### Membership Deleted Event

Input:

- WorkOS sends `organization_membership.deleted`.

Result:

- Matching `organization_user_relationships` row is soft-deleted.
- Matching `organization_role_assignments` rows for that `workos_user_id` are soft-deleted.
- If the relationship did not exist, a tombstone can be inserted by membership ID so stale replayed creates do not resurrect old state.

End state:

- The user no longer has an active organization relationship from that WorkOS membership.
- Role assignments from that membership are no longer active.

## User Event Created Or Updated

This path is handled by the WorkOS user event processor.

### User Has WorkOS `external_id`

Input:

- WorkOS user event has `external_id`.

Result:

- `external_id` is treated as the Gram user ID.
- `users` is upserted with WorkOS profile fields and timestamps.
- Pending `organization_role_assignments` for the WorkOS user are linked by setting `user_id`.
- Pending `organization_user_relationships` for the WorkOS user are linked by setting `user_id`.

End state:

- The local Gram user row exists and is linked to the WorkOS user.
- Any memberships and role assignments that arrived earlier are attached to that Gram user.

### User Has No WorkOS `external_id`, Existing Local User Exists

Input:

- WorkOS user event has no `external_id`.
- A local user already exists with matching `users.workos_id`.

Result:

- Existing local user ID is reused.
- `users` is updated from the WorkOS payload.
- Pending membership and role assignment rows are linked to that user.
- After the DB transaction commits, the processor attempts to set WorkOS `external_id` to the local Gram user ID.

End state:

- Local state is consistent even if the WorkOS `external_id` update fails.
- A warning is logged if updating WorkOS fails.

### User Has No WorkOS `external_id`, No Local User Exists

Input:

- WorkOS user event has no `external_id`.
- No local user exists with matching `users.workos_id`.

Result:

- A deterministic Gram user ID is generated from the WorkOS user ID.
- `users` is inserted with that generated ID.
- Pending relationships and role assignments are linked to that generated user ID.
- After commit, WorkOS `external_id` is updated to that generated Gram user ID.

End state:

- The WorkOS user has a local Gram user.
- Pending membership/role data becomes visible through normal Gram user/org joins.

### User Deleted Event

Input:

- WorkOS sends `user.deleted`.

Result:

- The local `users` row matching `workos_id` is disabled/soft-deleted using WorkOS timestamps.
- Organization memberships and role assignments are not directly changed by the user delete path.

End state:

- User state reflects the WorkOS delete.
- Membership cleanup remains driven by membership delete events or organization backfill snapshots.

## Organization Backfill

The WorkOS organization backfill is snapshot-based. It fetches:

- current WorkOS organization metadata
- current WorkOS organization roles
- current WorkOS organization users
- current WorkOS organization memberships

It then applies local state in this order:

1. Backfill organization metadata.
2. Backfill organization roles.
3. For each membership:
   1. Find the matching WorkOS user snapshot.
   2. Backfill that WorkOS user into `users`.
   3. If a Gram user ID was resolved, upsert the membership relationship.
   4. Sync role assignments for that membership.

### Organization Metadata

Input:

- WorkOS organization has a local row by `workos_id`, or it has `external_id` pointing at the Gram organization ID.

Result:

- Local organization metadata is updated from the WorkOS snapshot if the snapshot should win.

End state:

- Organization metadata reflects the current WorkOS organization snapshot.

If the organization is unknown and has no `external_id`, the org backfill skips it.

### Organization Roles

Input:

- WorkOS returns current organization roles.

Result:

- Current WorkOS organization roles are upserted locally.
- Local WorkOS-managed organization roles missing from the snapshot are soft-deleted.
- Grants for soft-deleted organization roles are removed.
- Global roles are backfilled separately before organization backfill runs.

End state:

- Local WorkOS-managed organization roles match the current WorkOS role snapshot.

### Membership With Resolvable User

Input:

- WorkOS membership exists in the snapshot.
- Matching WorkOS user snapshot exists.
- User backfill resolves a Gram user ID from either:
  - existing local `users.workos_id`, or
  - WorkOS `external_id`.

Result:

- User row is inserted/updated locally if needed.
- Membership relationship is upserted with `user_id`.
- Role assignments are synced with `user_id`.

End state:

- The Gram user has an active organization relationship.
- The Gram user has active role assignments matching the WorkOS membership snapshot.

### Membership With User Snapshot But No Resolved Gram User ID

Input:

- WorkOS membership exists in the snapshot.
- Matching WorkOS user snapshot exists.
- WorkOS user has no `external_id`.
- No local user exists with matching `users.workos_id`.

Result:

- Backfill logs a warning.
- Backfill does not create a deterministic local user ID.
- Backfill does not upsert the membership relationship.
- Backfill does not sync role assignments for that membership.
- Backfill does not update WorkOS `external_id`.

End state:

- Local DB is unchanged for that user/membership.
- The missing Gram user ID is treated as an operational warning to handle separately.

This is intentionally different from the user event path. Backfill only changes local database state; it does not create new local user identities when WorkOS has not already been linked to a Gram user.

### Membership Without Matching User Snapshot

Input:

- WorkOS membership snapshot references a user ID not present in the WorkOS org users snapshot.

Result:

- Backfill fails with an invariant error.

End state:

- No partial transaction is committed for that organization backfill.
- This state is treated as an inconsistent WorkOS snapshot or wrapper bug, not a normal skip case.

### Existing Local User Is Newer Than Backfill Snapshot

Input:

- Local `users.workos_updated_at` is newer than the WorkOS user snapshot timestamp.

Result:

- User row is not overwritten by the older snapshot.
- The resolved Gram user ID is still returned.
- Membership and role assignment snapshot can still be applied for that resolved user.

End state:

- User profile fields keep the newer local WorkOS state.
- Organization membership state still reflects the organization snapshot.

## Rejoin And Tombstone Cases

### User Leaves And Rejoins Same Organization

Input:

- Existing `(organization_id, user_id)` relationship was soft-deleted.
- WorkOS later sends or backfills a new active membership for the same user/org, usually with a new `workos_membership_id`.

Result:

- The existing tombstoned relationship is reused/reactivated.
- `workos_membership_id`, `workos_user_id`, WorkOS timestamp, and cursor fields are updated.
- A duplicate `(organization_id, user_id)` row is not inserted.

End state:

- The user has one active relationship for that organization.
- Historical tombstone state does not block rejoin.

### Pending Relationship Exists And User Later Syncs

Input:

- A membership event created a pending relationship with `user_id = NULL`.
- A tombstoned relationship also exists for the same `(organization_id, user_id)`.
- A later user event resolves the WorkOS user to that Gram user ID.

Result:

- The tombstoned relationship is re-linked/reactivated with the pending WorkOS membership fields.
- The pending placeholder row is soft-deleted.

End state:

- There is one active relationship for `(organization_id, user_id)`.
- The relationship points at the current WorkOS membership.

## Summary Matrix

| Path                                     | Gram user known? | Relationship result                     | Role assignment result                        |
| ---------------------------------------- | ---------------- | --------------------------------------- | --------------------------------------------- |
| Membership event before user event       | No               | Pending row with `user_id = NULL`       | Pending rows with `user_id = NULL`            |
| Membership event after user exists       | Yes              | Active row with `user_id`               | Active rows with `user_id`                    |
| User event after pending membership      | Yes after event  | Pending relationship linked             | Pending assignments linked                    |
| Backfill membership with resolvable user | Yes              | Active row with `user_id`               | Active rows with `user_id`                    |
| Backfill membership with no Gram user ID | No               | Skipped                                 | Skipped                                       |
| Membership deleted                       | Maybe            | Relationship soft-deleted or tombstoned | Assignments soft-deleted                      |
| User deleted                             | Existing user    | User soft-deleted                       | Membership/assignments unchanged by user path |
