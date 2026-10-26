---
"dashboard": patch
"server": patch
---

Agent identity setup now says what workload identity and federation mean instead of using the terms. A platform-signed credential is called a platform token, an agent's "Federated access" panel is now "Platform sign-in", the runs on a platform are called jobs, and their issuer is a token issuer. The `workload:read` and `workload:write` permissions are listed as "Trusted platforms".
