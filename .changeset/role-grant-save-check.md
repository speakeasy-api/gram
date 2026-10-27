---
"server": patch
"dashboard": patch
---

Removing an "All" permission from a role now also removes unrestricted grants stored by older versions, which previously stayed in place. The role editor now checks the permissions the server saved and shows an error, keeping Save enabled, instead of reporting "Role saved" when a permission change did not persist.
