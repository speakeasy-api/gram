---
"server": minor
---

Added a new API endpoint `/rpc/projects.get` to Gram server that allows clients to retrieve project details given a project slug. The project must exist within the organization referenced by the provided `gram-session` cookie or `Gram-Key` header.
