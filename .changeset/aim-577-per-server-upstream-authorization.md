---
"server": patch
---

Each MCP server now keeps its own upstream authorization server. Enabling User Identity on one server no longer re-points the other servers that share its organization sign-in, and a second OAuth-backed server no longer leaves every one of them reporting that no sign-in is needed while failing upstream. Signing in to a server also no longer requires connecting the providers its sibling servers use.
