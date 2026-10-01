---
"server": minor
"dashboard": minor
---

A trusted platform's page in the Access Hub gains a "Connect this platform" section. Pick an MCP server and it shows the values to enter in the platform's console: the token endpoint, the authorization server issuer with the audience rule, the MCP server URL and its API host. The values come from the new `workloadIdentities.connectionDetails` method, which derives them the same way the server's OAuth discovery document does, and a server whose token exchange cannot succeed says why instead of showing values that will not work.
