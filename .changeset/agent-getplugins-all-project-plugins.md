---
"server": patch
---

Return every published-project plugin to all org members from `agent.getPlugins`.

The endpoint previously returned only plugins assigned to the caller's exact
email or the org wildcard, so assignments via `role:`/`user:` principals never
reached a device — and there is no UI to create assignments yet. As an interim
step pending RBAC-backed assignment management, the per-principal assignment
filter (and the `@principal_urns` query param) is dropped: every non-deleted
plugin in the org's published projects is now returned to every org member.

The supplied email is still validated so the request contract is unchanged, and
the view's existing collapse handling keeps colliding-name and cross-org
isolation intact. No schema change.
