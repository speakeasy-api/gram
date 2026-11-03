---
"dashboard": patch
"server": patch
---

Changing upstream identity through the older organization-level provider pages now keeps a server's upstream credentials in step: removing a client from a server, removing a client from the organization, and deleting a platform client all retire the credentials those servers offered through it, in the same save. Organizations that configure upstream identity under Identity are sent there from the provider page instead of being offered the older per-server control.
