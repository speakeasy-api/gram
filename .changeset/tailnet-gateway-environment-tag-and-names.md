---
"server": patch
"dashboard": patch
---

The private network setup steps now show the Tailscale tag the environment's gateway actually uses, read from the API, in the tailnet policy snippet, the OAuth client step and the approval help. Gateway machines can carry an environment qualifier (`speakeasy-gw-<qualifier>-<slot>`) so two environments sharing a tailnet register devices that can be told apart; production names are unchanged.
