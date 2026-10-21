---
"server": patch
---

Resolve issuer-gated direct remote and tunneled servers through typed auth clients behind the `auth-client-resolver` flag, with `auth-client-resolver-shadow` comparing the two paths without changing requests. Choices not yet mapped onto auth clients, and cases the old path still handles differently, keep being served the old way. With the flags off nothing changes.
