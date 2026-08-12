---
"hooks": minor
---

Forward requestable-block metadata to the device-agent socket. When the server
denies a tool call against an unapproved (shadow) MCP server, the relay now
posts the structured block effect — request token, server, policy, expiry — to
the local Speakeasy device agent (best-effort, 300ms budget, never on the
allow path), so the agent can surface a native "request access" flow instead
of the user fishing the bypass link out of the deny prose. Machines without
the agent, or with an older agent, are unaffected: the notify swallows every
failure and the deny is unchanged.
