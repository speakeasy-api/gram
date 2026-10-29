---
"server": minor
"dashboard": patch
---

Device agents can report their applied state with `agent.reportState`: the policy etag they enforce, each tool's enforcement outcome, their versions, and their update posture. Speakeasy stores the latest report per user per machine, and records the etag it serves each user so a device's attested state can be checked against it.
