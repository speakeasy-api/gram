---
"server": patch
"dashboard": patch
---

A remote session client's own scopes now take precedence over its identity provider's scope override. The override is requested only for clients that set no scopes of their own, so a client that sets scopes under a provider with an override now requests its own scopes, plus the standard scopes the provider advertises. Clearing a client's scopes in the identity provider sheet now hands the request back to the override. Automatic dashboard and Platform MCP attachment leave client scopes unset when the provider has an override, while explicit manual scopes still win. Editing the provider URL preserves the client's stored scopes unless the operator explicitly edits them.
