---
"server": patch
---

Platform MCP risk policy tools no longer accept or return the policy-level `message_types` field. New policies leave the legacy scope columns empty and scope through `detection_scopes`; a stored policy whose legacy `message_types` still narrows it is reported as `raw_scope` until the legacy scope migration folds it.
