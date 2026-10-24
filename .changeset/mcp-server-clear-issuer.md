---
"server": minor
---

`mcpServers.update` accepts `clear_user_session_issuer` to detach a server's user session issuer, for hosted, remote, tunneled and unproxied servers. It cannot be combined with `user_session_issuer_id`, and is refused while identity-chaining bindings depend on the issuer. A remote or tunneled server whose issuer was detached no longer gets a new one minted on later saves.
