---
"server": minor
"dashboard": minor
---

Redesign the Plugins pages and add MCP server readiness surfacing:

- Marketplace card now reflects real setup state: an uninitialized/warning
  variant (skeleton repo link, "Not published" badge, "Publish now"/"Add
  collaborators" CTA) shown until the marketplace repo exists **and** has at
  least one collaborator who has accepted their GitHub invite, distinct from
  the connected/published state.
- Install flow reworked: a single "Install" dropdown (GitHub installation via
  marketplace, preferred, or direct zip download) replaces the old split
  button, on both the Plugins index and detail pages, and no longer disables
  zip download just because the marketplace isn't set up yet.
- Default plugin gets special treatment (badge, description, auto-heal on
  read for projects that predate the feature) and plugin membership no
  longer N+1-queries its servers.
- New collapsible readiness bar on the MCP server ("x" route) sidebar,
  summarizing Server URL / Authentication / Source / Included in Plugin
  status with links to fix each.
- Server: `GetPublishStatus` now reports whether the marketplace repo has a
  real (accepted, not just invited) collaborator, cached briefly to avoid
  hitting GitHub's API on every dashboard poll, and invalidated immediately
  after publishing adds one.
