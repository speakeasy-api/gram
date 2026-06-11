---
"server": patch
---

Fix the prompt-based risk policy feature flag (`gram-prompt-policies`) being
treated as disabled for orgs that enabled it via a PostHog group. The backend
now forwards org/project group memberships when evaluating the flag, so
group-targeted releases match server-side the same way they do in the
dashboard — unblocking policy create/update and enforcement.
