---
"server": minor
---

`mcpServers.update` accepts `clear_user_session_issuer` to detach a server's user session issuer, for hosted, remote, tunneled and unproxied servers, whether the issuer belongs to the project or the organization. It cannot be combined with `user_session_issuer_id`, and is refused while identity-chaining bindings in the project depend on the issuer. Saving a remote or tunneled server without an issuer mints one only when the request switches it onto a new remote or tunneled backend, from another remote backend or from a toolset or unproxied one; saving it on the same backend keeps it without an issuer.
