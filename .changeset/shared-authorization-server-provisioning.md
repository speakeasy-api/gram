---
"server": minor
---

Organization administrators can create a shared authorization server for their organization through a new management API operation and a matching Platform MCP tool. The issuer is created with general-purpose defaults and its issuer URL pinned on this deployment, retries and concurrent submissions with the same idempotency key resolve to the same issuer, and every issuer now reports its authorization server mode and pinned issuer URL.
