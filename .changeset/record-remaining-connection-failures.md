---
"server": patch
---

Connection health now records three more kinds of failed connection. Each record says what failed and who needs to fix it. What the client receives is unchanged.

- When a workload or an assistant asks Speakeasy for a token, the request is recorded, whether it worked or not.
- When a hosted server's tool call is refused by the service with a 401, or a 403 that says the token is invalid, the record says whose credential was refused: the person's sign-in, the organization's shared sign-in, or a saved key.
- When a direct server rejects the sign-in on `initialize`, `tools/list`, or any other request, not only on tool calls, the record names the request. A rejection counts only when the request that reached the server carried the credential, and a 403 counts only when it says the token is invalid. A failed renewal of a server's own credential during a provider outage is recorded as that outage.
