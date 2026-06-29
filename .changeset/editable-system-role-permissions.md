---
"server": minor
"dashboard": minor
---

Allow editing the permissions of system roles (`admin`/`member`) per organization, while keeping their name and description platform-managed. The Admin role is guarded against losing the `org:admin` permission to prevent org lockout. The roles tab is reworked: the whole role row opens the edit sheet (gated on `org:admin`), scope groups no longer auto-expand and show a description when collapsed, and the members column uses a new interactive member facepile (hover focus, click to view all members) that also replaces the facepile on the org home projects list. Adds Directory Sync (SCIM) info alerts on the team, roles, and identity pages explaining that members and roles are managed by the identity provider while SCIM is enabled.
