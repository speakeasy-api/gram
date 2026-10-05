---
"server": minor
---

Manage the hooks version rollout from the admin dashboard. Staff set a platform-wide default pin and per-organization overrides naming the highest hooks plugin version an organization may receive, on the new Hooks rollout page and each organization's Features tab. The plugin publisher reads these pins instead of the `hooks-rollout` PostHog flag, which it still falls back to for organizations without an override until a default pin is set. The staff Admin MCP gains read-only `get_hooks_rollout` and `get_organization_hooks_rollout` tools.
