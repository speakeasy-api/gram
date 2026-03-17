# Architecture Notes

## Org vs Project Scoping (2026-03-12)

### Taxonomy

- `org/team` > `projects`
- Users can only be a member of one org (for now)

### Settings Scope Analysis

Settings are routed under `/:orgSlug/:projectSlug/settings`, implying project-level scoping, but the backend implementations almost universally use `authCtx.ActiveOrganizationID`. The `Gram-Project` header, when present, is only used for auth middleware resolution.

| Settings Area         | Route                | Actual Scope       | Notes                                                                                     |
| --------------------- | -------------------- | ------------------ | ----------------------------------------------------------------------------------------- |
| Custom Domain         | `/settings/domains`  | **Org-scoped**     | `repo.GetCustomDomainByOrganization` — UI says "one custom domain per organization"       |
| API Keys              | `/settings/api-keys` | **Org-scoped**     | `repo.ListAPIKeysByOrganization` — Goa design doesn't even require `ProjectSlug` security |
| Billing               | `/billing`           | **Org-scoped**     | `billingRepo.GetPeriodUsage`, `CreateCheckout`, `CreateCustomerSession` all use org ID    |
| Logging & Telemetry   | `/settings/logs`     | **Org-scoped**     | `productfeatures` service uses `authCtx.ActiveOrganizationID` for both get and set        |
| Team                  | N/A                  | **Org-scoped**     | No pages/endpoints exist yet — currently handled by Speakeasy IDP                         |
| General / Danger Zone | `/settings/general`  | **Project-scoped** | Project deletion — the only genuinely project-level setting                               |

### Key Observations

1. Some org-scoped endpoints (domains, telemetry, billing) still declare `Security(security.Session, security.ProjectSlug)` in Goa design files, even though the project slug is only used for auth context resolution and ignored by service implementations.
2. This creates a misleading coupling — the frontend must select a project to access settings that have nothing to do with that project.
3. These settings could live under an org-level route (e.g. `/:orgSlug/settings/...`) without any backend changes, since the backend already treats them as org-scoped.

### Relevant Backend Files

- `server/internal/customdomains/impl.go`
- `server/internal/keys/impl.go`
- `server/internal/usage/impl.go`
- `server/internal/productfeatures/impl.go`
- `server/design/domains/design.go`
- `server/design/keys/design.go`
- `server/design/usage/design.go`
- `server/design/productfeatures/design.go`
