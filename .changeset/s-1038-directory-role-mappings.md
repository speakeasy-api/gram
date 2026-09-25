---
"server": minor
---

Add directory role mappings to the access API. Organization admins can map a directory group, or a directory attribute value such as `department_name=Sales`, to a role, and members who match get that role on top of the roles assigned to them directly. `syncDirectoryGroups` pulls groups from the organization's linked directories so they can be mapped before any directory event delivers them. Setting and removing a mapping are audited under the `directory_role_mapping` subject, and role member counts include members who hold a role only through a mapping.
