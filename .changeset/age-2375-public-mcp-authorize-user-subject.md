---
"server": patch
---

Public-MCP `/authorize` accepts a new `requireUserIdentity=1` query parameter that forces the caller through the IDP so the resulting session is bound to a user subject rather than an anonymous one. Without the parameter, public-toolset `/authorize` continues to mint an anonymous subject regardless of ambient cookies or Bearer tokens. Callers from outside the endpoint's organization receive a 403 from the IDP callback — public toolsets that need cross-organization access should omit the parameter and use anonymous sessions.

The assistant runtime sets the parameter when initiating MCP authorization flows against Gram-served endpoints so subsequent tool calls can be attributed to the user. Foreign (non-Gram) authorization endpoints discovered via `.well-known/oauth-authorization-server` do not receive the parameter.
