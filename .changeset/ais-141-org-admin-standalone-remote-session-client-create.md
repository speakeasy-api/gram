---
"server": minor
"dashboard": minor
---

Org admins can now register a standalone `remote_session_client` directly from the Remote Identity Provider details page. A new `organizationRemoteSessionIssuers.createClient` endpoint creates a client under an existing issuer with no `user_session_issuer` attachments; the client inherits a project-specific issuer's project, or the admin names a project (downscoping) when the issuer is organization-level. The dashboard surfaces a `New Client` button on the issuer's Clients tab that opens a sheet supporting Dynamic Client Registration (when the issuer advertises a `registration_endpoint`) or manual `client_id` / `client_secret` entry.
