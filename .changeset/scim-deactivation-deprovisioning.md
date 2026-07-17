---
"server": patch
---

Deprovision user access on SCIM deactivation. WorkOS `organization_membership` events with status `inactive` and `dsync.user` events with a non-active state now soft-delete the user's organization relationship and role assignments and invalidate their cached user info. Login-time and backfill membership syncs only import active memberships, directory-user upserts no longer resurrect soft-deleted rows unless the incoming state is explicitly active, and organization rosters exclude deleted users.
