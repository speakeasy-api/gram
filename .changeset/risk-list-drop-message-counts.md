---
"server": minor
---

The `pending_messages` and `total_messages` fields on `RiskPolicy` are now optional and omitted from `risk.listPolicies` responses. Computing them re-aggregated every risk result for the project on each list call, and no consumer read them from the list. Single-policy responses (`risk.getRiskPolicy`, create/update) still populate both fields, and analysis progress remains available via `risk.getRiskPolicyStatus`.
