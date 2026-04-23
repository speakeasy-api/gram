---
"server": patch
---

Fix a data race in concurrent OpenAPI tool extraction that could corrupt schemas or crash deployments when the same schema was referenced by multiple operations.
