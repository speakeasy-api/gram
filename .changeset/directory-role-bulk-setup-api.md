---
"server": minor
---

Add `previewDirectoryRoleSetup` and `applyDirectoryRoleSetup` for setting up directory role mappings in bulk. The preview proposes a role for every directory group and attribute value: an existing role with the same name, or a new one named after the source. Attribute keys with too many values to offer are listed by name and value count. Apply creates the new roles and replaces each listed source's roles in one transaction, and changes nothing if a source or its roles changed since the preview. New roles get no grants unless a grant set is given. WorkOS role sync is reported as requested, not done, and creating roles sets up no plugin distribution.
