---
"server": patch
---

Public-MCP `/authorize` accepts a new `requireUserIdentity=1` query parameter that forces the caller through the IDP so the resulting session is bound to a user subject rather than an anonymous one. Without the parameter, public-toolset `/authorize` continues to mint an anonymous subject regardless of ambient cookies or Bearer tokens — this prevents cross-site requests from de-anonymising a logged-in user's session against an attacker-controlled OAuth client.

The assistant runtime sets the parameter when initiating MCP authorization flows against Gram-served endpoints so subsequent tool calls can be attributed to the user. Foreign (non-Gram) authorization endpoints discovered via `.well-known/oauth-authorization-server` do not receive the parameter.

Cross-org user subjects (only producible on public toolsets, where the IDP flow admits any authenticated user regardless of the endpoint's organization) are no longer stamped with the endpoint's organization metadata in the runtime auth context — they are treated as anonymous-equivalent for org-scoped fields, mirroring the existing behaviour for true anonymous subjects.
