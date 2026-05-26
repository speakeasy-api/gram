---
"dashboard": patch
---

Redesign the org home page with a two-column layout. Left rail shows compressed Recent challenges (color-coded deny pills, sidebar-friendly width) and Recent activity (sharing the audit-log action color scheme). Right column shows projects as a thin rectangular stack (list view) or a 1/2/3-column card grid (grid view, toggle persisted in localStorage). Each project row/card shows the most recent audit-log action with a hover tooltip for full UTC + local timestamps, a facepile of active contributors (up to 10, sourced from audit-log actors with a deterministic earliest-by-joinedAt fallback), a star to favorite/unfavorite (stored client-side per org), and a kebab menu (favorite toggle, project settings, view audit logs, copy slug). New "Add New" dropdown next to the search bar offers Project / Team member / Role, gated by `org:admin`. Favorites surface in their own section above an "All projects" divider when present.
