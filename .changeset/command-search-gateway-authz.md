---
"server": patch
---

Fix Command K resource search failing for members whose access is enforced by roles. The search asked whether the member could list gateways without naming the project, which the authorization engine rejects, so every search returned an error and the palette fell back to loading full resource lists.
