---
"server": patch
---

Add guarded staff Admin MCP changes to a single organisation's SSO and SCIM setup entitlements, with exact-target browser approval, stale-state checks, retry-safe receipts and transactional audit logging. These changes allow or deny creation of setup portal links without returning those links, changing existing SSO connections or directory sync, or modifying membership, roles, billing or trials. Existing customer Platform MCP tools and demo data remain unchanged because these entitlements are staff-managed.
