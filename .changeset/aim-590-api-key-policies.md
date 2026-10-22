---
"server": minor
"dashboard": patch
---

API keys can be created with their own policy through `policy_grants`: `mcp:connect` and `mcp:auth` grants and their blocks. A project-bound key's grants must stay in its project, and a server grant covering every server must name a project. Setting a policy requires org admin and a consumer, producer or chat scope; `keys.list` returns it, the creation and revocation audit entries record it, and revoking the key removes it. The policy is not enforced yet.
