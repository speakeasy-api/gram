---
"server": minor
"dashboard": patch
---

Add optional `name` (display name) and `logo_asset_id` to remote session issuers across both the project-scoped (`remoteSessionIssuers`) and organization-scoped (`organizationRemoteSessionIssuers`) services. On create, `name` is trimmed and stored as NULL when empty; on update it follows the same three-state semantics as the nullable endpoint fields (omitted keeps, empty string clears). `logo_asset_id` is set-only for now (no clear path, no upload UI yet). The dashboard renders the display name as the primary label with the issuer URL as the secondary line, exposes an optional Display name input on the attach and modify sheets, and renders a logo when one is present. On the attach sheet the Display name auto-derives from the Issuer URL hostname until the operator edits it, matching the existing Slug behavior.
