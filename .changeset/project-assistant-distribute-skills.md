---
"server": patch
---

The project assistant can now distribute the skills it creates. `platform_distribute_skill` and `platform_undistribute_skill` attach a skill to a plugin or assistant and revoke it again, and `platform_list_plugins` resolves a plugin by name to the ID they take. All three reuse the same permissions, feature gating, and audit logging as the dashboard.
