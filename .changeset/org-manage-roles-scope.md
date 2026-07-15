---
"server": minor
"dashboard": minor
---

Add an `org:manage_roles` scope for delegated role administration. A custom role holding only this scope can manage roles, member-role assignments, and identity-provider (SSO) group-to-role mappings, without access to Observe (cost/sessions), Secure (risk), billing, or any project. `org:admin` satisfies it via scope expansion, so existing admins are unaffected.
