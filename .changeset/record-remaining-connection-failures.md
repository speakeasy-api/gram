---
"server": patch
---

Connection health now records three more kinds of failed connection. Each record says what failed and who needs to fix it. Apart from the inference token change below, what the client receives is unchanged.

- When a workload or an assistant asks Speakeasy for a token, the request is recorded, whether it worked or not. This includes the shared sign-in address that serves all of an issuer's servers. Refused requests from one workload issuer are grouped, so tokens with new names can't flood the history. A grouped record that stands for several requests doesn't name an identity. A workload's request for an inference token now checks its assertion before its project, as the platform token request does, so a request for a project outside the issuer is recorded too. An invalid assertion is now refused as invalid even when the project is also wrong.
- When a hosted server's tool call is refused by the service with a 401, or a 403 that says the token is invalid, the record says whose credential was refused: the person's sign-in, the organization's shared sign-in, or a saved key. Nothing is recorded when the caller supplied the credential or chose the server's address.
- When a direct server rejects the sign-in on `initialize`, `tools/list`, or any other request, not only on tool calls, the record names the request. A rejection counts only when the request that reached the server carried the credential, and a 403 counts only when it says the token is invalid. Servers behind a gateway follow the same rules. A failed renewal of a server's own credential during a provider outage is recorded as that outage.
