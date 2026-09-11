---
"server": patch
"dashboard": patch
---

The risk policy create and update endpoints no longer accept the policy-level `message_types`, `scope_include`, and `scope_exempt` fields. New policies leave those legacy columns empty and updates carry stored values forward unchanged; scope through `detection_scopes` instead. The fields remain readable on a policy until the legacy scope migration folds them. The dashboard's secrets guide now scopes its default policy through a detection scope.
