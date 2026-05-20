# Internal MCP Server Portals — Design

Date: 2026-05-20
Linear: [Internal MCP Server Portals](https://linear.app/speakeasy/project/internal-mcp-server-portals-400b2f3f4499/overview)
Status: Draft (awaiting review)

## Problem

Customers want a single, internal-facing landing page per project that lists every MCP server the project hosts, with each server's tools and install/connection options accessible from one place. Today, the install/connection experience exists _per_ MCP server (`MCPHostedPage`), but there is no aggregated "cover page" that bundles them. Linear's project brief proposes hosting this at the root of a custom domain (e.g. `mcp.acme.com`); the v1 of this design ships without the custom-domain requirement and serves the portal at a Gram-hosted path.

## Goals

- One shareable, org-internal URL per project that lists all of the project's MCP servers as cards.
- Each card surfaces enough information (name, description, tool count) to identify the server and a "View install" affordance that links to the existing per-server install page.
- Design-partner-quality first impression: project branding (logo, display name, tagline) is configurable from project settings.
- Reuse existing primitives wherever possible: `mcp_servers`, `mcp_endpoints`, `toolsets`, `assets`, the existing session/RBAC machinery.

## Non-goals (deferred)

- Custom domain attachment so the portal serves at the apex of a customer-owned domain (`mcp.acme.com`). Tracked as a follow-up.
- Public / unlisted visibility modes (anyone-with-link). v1 is org-internal, auth-required only.
- Per-server "publish to portal" toggle, curation, ordering, sectioning. v1 lists _all_ non-deleted MCP servers in the project.
- View analytics (card clicks, page views).
- Audit log entry for portal edits.
- Theme color or layout customization beyond logo + display name + tagline.
- Programmatic discovery (`.well-known/mcp-portal.json`).

## Decisions (locked in brainstorming)

| Decision                | Choice                                                                              | Rationale                                                                                          |
| ----------------------- | ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Audience                | Org-internal, auth required                                                         | Closest fit to "internal-facing catalogue"; reuses session/IDP.                                    |
| Membership              | All MCP servers in the project, automatically                                       | No publish workflow needed for v1; portal is a filtered view of `mcp_servers`.                     |
| URL                     | Gram-hosted at `app.gram.dev/portal/{project-slug}`                                 | No custom domain dependency. Custom-domain mode deferred.                                          |
| Branding                | Logo, display name, tagline included in v1                                          | Design-partner quality requires non-generic first impression.                                      |
| Storage                 | New `project_portals` table                                                         | Dedicated home; avoids muddying `project_marketplace_settings` (public marketplace concept).       |
| Admin UI                | Inside existing **project settings** page (no dedicated nav entry)                  | The portal is a project property, not its own workspace area.                                      |
| Disabled state          | Hard 404 for org members; project admins can preview via `?preview=1` from settings | Simplest external behaviour while keeping the in-settings preview iframe functional before launch. |
| Default `enabled`       | `false` for all projects (no backfill)                                              | Opt-in; existing projects do not start serving a portal silently.                                  |
| Card description source | `mcp_servers` description if present, else fall back to `toolsets.description`      | No new column required in v1.                                                                      |

## Data model

One new table:

```sql
CREATE TABLE IF NOT EXISTS project_portals (
  id              uuid NOT NULL DEFAULT generate_uuidv7(),
  project_id      uuid NOT NULL,
  enabled         boolean NOT NULL DEFAULT false,
  display_name    TEXT CHECK (display_name IS NULL OR (display_name <> '' AND CHAR_LENGTH(display_name) <= 64)),
  tagline         TEXT CHECK (tagline IS NULL OR CHAR_LENGTH(tagline) <= 200),
  logo_asset_id   uuid,
  created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at      timestamptz NOT NULL DEFAULT clock_timestamp(),

  CONSTRAINT project_portals_pkey PRIMARY KEY (id),
  CONSTRAINT project_portals_project_id_key UNIQUE (project_id),
  CONSTRAINT project_portals_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE,
  CONSTRAINT project_portals_logo_asset_id_fkey FOREIGN KEY (logo_asset_id) REFERENCES assets (id) ON DELETE SET NULL
);
```

Semantics:

- A row is **created lazily** the first time an admin opens portal settings or calls `updatePortal` — projects without a row are treated as portal-disabled.
- NULL `display_name` / `logo_asset_id` fall back to `projects.name` / `projects.logo_asset_id` at read time. NULL `tagline` renders no tagline.
- `enabled = false` ⇒ the public portal route returns 404; project-settings UI can still read/write the row.

Generated migration goes in `server/migrations/<ts>_create_project_portals.sql` via `mise db:diff create_project_portals`.

## Backend

### Goa service

New service at `server/design/portals/design.go` named `portals`, served under `/rpc/portals.*`:

| Method         | URL                                   | Auth                                     | Result                                                                                                                           |
| -------------- | ------------------------------------- | ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `getPortal`    | `GET /rpc/portals.get?project_slug=…` | Session, org member of the project's org | Portal config (resolved with fallbacks) **plus** the rendered card list.                                                         |
| `updatePortal` | `POST /rpc/portals.update`            | Session, project admin scope             | Upserts the portal row; returns the updated config. Fields: `enabled`, `display_name`, `tagline`, `logo_asset_id`. All optional. |

`getPortal` returns:

```
{
  portal: {
    enabled: bool,
    display_name: string,           // resolved (overlay falls back to project.name)
    tagline: string | null,
    logo_url: string | null,        // resolved asset URL
    project_slug: string,
  },
  servers: [
    {
      slug: string,
      name: string,
      description: string | null,   // server description, else toolset.description
      tool_count: int,
      install_url: string,          // mcp_endpoints URL for the per-server install page
    },
    ...
  ]
}
```

Card data is built by joining `mcp_servers` → `mcp_endpoints` → `toolsets` and counting tools per toolset. Servers with no `mcp_endpoint` are omitted (no install link to surface).

### Auth path (handlers)

1. Resolve project by `project_slug` within the session's org. If the slug belongs to another org or does not exist, return **404** uniformly (never 403) — do not leak project existence to other orgs.
2. For `getPortal`: load the `project_portals` row. If absent **or** `enabled = false`, return **404** — unless the caller has the project-admin scope **and** the request sets `preview=true` (so the in-settings preview iframe works before launch). The public route inherits this behaviour.
3. For `updatePortal`: require the existing project-admin scope (whatever today's project settings page checks). Upsert the row by `project_id`.

No new RBAC scopes are introduced. All authorization is expressed via the existing project-scope grants and enforced through `authz.Engine.Require`.

### File layout (server)

```
server/design/portals/design.go
server/internal/portals/
  impl.go
  queries.sql
  repo/                    # generated by sqlc
  rbac_test.go
  getportal_test.go
  updateportal_test.go
  setup_test.go
server/database/schema.sql                                 # +project_portals
server/migrations/<ts>_create_project_portals.sql          # generated
```

## Frontend

### Public-facing portal route

Path: `app.gram.dev/portal/{project-slug}` (the deliverable link).

- React route registered in `client/dashboard/src/routes.tsx`.
- If unauthenticated, redirect to `/login?return_to=/portal/{slug}` (existing pattern).
- After login, call `getPortal({project_slug})`.
- On 404, render a generic "Portal not found" page (do **not** distinguish "doesn't exist" from "disabled" from "wrong org").
- On success, render:
  - Header band: resolved logo, `display_name`, `tagline`, and a discreet "Powered by Gram" footer.
  - Responsive grid: one `PortalCard` per server.
  - Empty state if the project has no MCP servers, linking to the catalog.

### Admin UI (inside project settings)

Add a "Portal" section to the existing project-settings page. No new top-level nav entry.

Contents:

- `Enabled` toggle (default off). When on, a "Copy portal URL" button surfaces.
- Inputs: `display_name`, `tagline`, logo upload (re-uses existing asset upload component).
- Right-side live preview: iframe pointing at `/portal/{project-slug}?preview=1`. While editing (before `enabled` is flipped on), the preview iframe stays usable because `getPortal` honours `preview=1` for project admins. After `enabled` is flipped on, the same URL shows what every org member sees.

### File layout (frontend)

```
client/dashboard/src/pages/portal/
  PortalPage.tsx           # /portal/:projectSlug — public-facing portal
  PortalCard.tsx           # one MCP server card
  PortalPreview.tsx        # iframe preview shared by admin UI
  hooks.ts                 # SDK hook wrappers
client/dashboard/src/pages/settings/
  PortalSettings.tsx       # new section embedded in existing settings page
client/dashboard/src/routes.tsx  # register /portal/:projectSlug
```

## URL & routing summary

| Surface                     | URL                                                          | Notes                                   |
| --------------------------- | ------------------------------------------------------------ | --------------------------------------- |
| Portal (the shareable link) | `app.gram.dev/portal/{project-slug}`                         | Auth-gated React route.                 |
| Per-server install pages    | `app.gram.dev/mcp/{server-slug}` (existing `installPageUrl`) | Unchanged; portal cards deep-link here. |
| Admin (edit portal)         | Embedded in existing project settings page                   | Project-admin scope.                    |

`projects.slug` is unique per organization, so collisions across orgs cannot occur at the lookup step (since lookup is always scoped to the requesting session's org).

## Implementation order

1. **Migration PR** — `project_portals` table + `mise db:diff` + `mise db:hash`. Ships alone (see CLAUDE.md migration rules).
2. **Backend PR** — Goa service, handlers, SQLc queries, RBAC tests, SDK regen.
3. **Frontend PR** — admin section in project settings, public-facing portal route, preview iframe. Includes UX polish for empty states and error handling.

Splitting migration first reduces blast radius and matches the project convention.

## Skills to activate during implementation

`postgresql` (schema + migration generation), `gram-management-api` (new Goa service wiring), `gram-rbac` (handler-side auth checks), `golang` (handler implementation), `frontend` (React pages, route registration), `pr` (per-PR description).

## Open questions

None outstanding; all clarifications from the brainstorming pass are folded into "Decisions" above.

## Risks / unknowns to verify during implementation

- The exact name of the existing project-admin scope (verify in `server/internal/authz` during implementation).
- Whether `mcp_endpoints` URLs need any rewriting when surfaced through the portal (custom-domain handling), or if the existing `installPageUrl` resolver already produces the correct absolute URL.
- Whether logo `assets` already expose a public URL helper, or if a new resolution path is needed.
