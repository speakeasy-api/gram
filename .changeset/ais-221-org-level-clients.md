---
"server": minor
"dashboard": minor
---

Support organization-level remote session clients. A `remote_session_client` can now be created with no project (organization-level) so every project in the organization can attach and use it, mirroring organization-level remote session issuers. On `organizationRemoteSessionIssuers.createClient` and `createCimdClient` an omitted `project_id` under an organization-level issuer creates an organization-level client (the same `project_id`-omission convention `createIssuer` already uses), while a supplied `project_id` scopes the client to that project. The consent/token runtime resolver, the project-scoped client reads, and the attach-time single-client invariant now resolve both a project's own clients and organization-level clients in its organization, so a project admin can attach, detach, and use an organization-level client from their own user session issuer but cannot edit or delete it (those stay on the org-admin surface). The `RemoteSessionClient` API shape adds `organization_id` and allows an empty `project_id` for organization-level clients, mirroring the issuer change.
