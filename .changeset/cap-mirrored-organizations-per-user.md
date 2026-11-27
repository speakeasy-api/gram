---
"server": patch
---

Limit each user to one organization in a single shared rule. Self-serve organization creation refuses a user who already has one, and an organization that arrives from the identity provider for such a user is disabled and recorded as `organization:disabled_by_membership_cap`.
