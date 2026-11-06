---
"server": patch
"dashboard": patch
---

Speakeasy now asks for a review before sensitive access is granted and records it when it is. In the role editor, a save that adds a permission, or adds members or agents to a role that already grants a sensitive one, opens a review step that lists what is added and what the new holders gain, and waits for a confirmation when any of it exposes other members' activity or allows destructive changes. Manage Roles and Grant Access preview what a role adds to what the member already holds, counting a permission held on one project that the role gives everywhere, and wait for the same confirmation. The `access_role:create`, `access_role:update` and `access_member:update_role` audit rows carry `sensitive_scopes_added` in their metadata, naming the sensitive permissions the change gave that were not held before or were held only on some resources; a change that adds nothing sensitive writes no metadata.
