---
"server": minor
"dashboard": minor
---

Adds **Internal MCP Server Portals** — a per-project, org-internal catalogue page reachable at `app.gram.dev/portal/{project-slug}` that lists every MCP server in the project as cards (server name, description, tool count, link to the existing per-server install page). Project admins toggle the portal on in project settings and brand it with a logo, display name, and tagline.

Surface area:

- New `project_portals` table; one row per project, `enabled = false` by default so no existing project starts serving a portal silently.
- New `portals` Goa service: `getPortal` (org-member read; returns the resolved config plus enriched server cards) and `updatePortal` (project-admin write; read-then-merge partial-update semantics — `nil` preserves, `""` clears, non-empty sets). Disabled portals return 404 to org members; project admins can preview via `?preview=true`.
- Dashboard route `/portal/:projectSlug` (lazy-loaded, auth-gated by `LoginCheck`, 404s uniformly on any failure) plus a new "Internal MCP Portal" section embedded in project settings.
- No new RBAC scopes — reuses `ScopeProjectRead` / `ScopeProjectWrite`.
