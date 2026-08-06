---
"server": minor
"dashboard": minor
---

Platform admins can now curate the shared remote identity provider catalog from the dashboard, under a new Platform Admin section in the sidebar: list, create, edit, refresh discoverable metadata, and delete the providers that every organization inherits. The listing reports platform-owned and tenant-owned client counts separately, so a delete that will be refused says up front which blockers the admin can clear and which belong to an organization. `adminRemoteSessions.listGlobalIssuers` and `adminRemoteSessions.getGlobalIssuer` now return both counts alongside the issuer. Organizations can register a client against an inherited platform provider straight from their own provider list.
