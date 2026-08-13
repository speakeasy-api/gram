---
"server": patch
---

Apply the phased hooks rollout gate to the publish freshness read. A hooks
generator version bump used to flip every rollout-gated org's publish status to
"needs syncing" permanently: publishing carries a gated org's hooks subtree
verbatim (by design), so the stored hooks version could never reach the current
constant that GetPublishStatus compared against. The status read now runs the
same eligibility check as the publish path and skips the hooks version/config
comparison for orgs the rollout hasn't cleared — their published hooks are
their target, so only MCP content changes count against freshness. Cleared
orgs still read a pending hooks bump (or a deferred hook-config change) as
stale, prompting the publish that applies it.
