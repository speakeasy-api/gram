---
"server": minor
"dashboard": minor
---

A gateway's Debug tab shows its current client admission setting: which apps can sign in with a client ID metadata document, the first custom document URLs allowed in known-apps-only mode, and a link to the gateway's authentication settings. It is the setting in force now, not when existing connections were admitted. A new read-only Platform MCP tool, `get_gateway_client_admission`, returns the same setting for one exact gateway.
