---
"server": minor
---

Function deployments now prefer the operator-set `memory_mib_override` / `scale_override` columns over the config-driven memory and scale, and carry those overrides forward across redeploys so they are not reset by a later customer deploy.
