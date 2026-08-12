---
"server": patch
---

Record an actor display name on audit entries written from organization-less sessions. `sessions.Authenticate` now populates the actor email for sessions with no active organization, and enterprise-trial arming threads the actor email through provisioning so the self-signup callback — which has no auth context — still attributes the `organization:enterprise_trial_armed` entry instead of storing a bare actor id.
