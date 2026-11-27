---
"server": patch
---

Disable an organization mirrored from the identity provider when its only member already has another enabled organization, and record it as `organization:disabled_by_membership_cap`. Platform MCP connections now stop working when their organization is disabled.
