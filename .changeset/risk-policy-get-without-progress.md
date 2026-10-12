---
"server": patch
---

Risk policy details load quickly. `risk.getPolicy` and the policy create/update responses no longer count the project's chat messages before returning, which made the first load of a policy detail page take several seconds in large projects. The `pending_messages` and `total_messages` fields on a risk policy are now deprecated and always omitted; `riskPoliciesStatus` still reports analysis progress.
