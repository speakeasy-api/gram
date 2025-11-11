---
"server": patch
---

Fixed a bug in `ExecuteProjectFunctionsReaperWorkflow` where it was running the
wrong workflow (`ProcessDeploymentWorkflow` instead of
`FunctionsReaperWorkflow`).
