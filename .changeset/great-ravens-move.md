---
"server": patch
---

Updated the function deployment temporal activity so it spawns multiple goroutines to deploy functions in parallel. This should in theory speed up deployments with several functions.
