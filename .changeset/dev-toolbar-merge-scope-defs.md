---
"dashboard": patch
"server": patch
---

Fix two related gaps that surfaced after the `environment:read`/`environment:write`
scopes shipped:

- Dev toolbar: `loadState` now merges `SCOPE_DEFS` defaults into any persisted
  override state. Previously, scopes added to `SCOPE_DEFS` _after_ a user had
  already saved override state would render as "checked" via a per-row fallback
  but were absent from `state.scopes`, so `getRBACScopeOverrideHeader` silently
  omitted them — leaving the kebab disabled despite looking enabled.
- `access.listGrants`: the `allScopesGrants()` fallback (used for non-enterprise
  orgs and admin impersonation) now includes `environment:read` and
  `environment:write`. Without them the dashboard's effective-grants surface
  was missing two valid scopes.
