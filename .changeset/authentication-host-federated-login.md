---
"server": patch
---

Federated login now completes for MCP servers whose issuer opts in with `use_authentication_host`. The browser stays bound to the consent page on the authentication host, upstream logins return there, and sign-ins started on the MCP host, such as connecting from the dashboard, move to the authentication host first. A sign-in in progress when an issuer turns `use_authentication_host` on or off must be restarted.
