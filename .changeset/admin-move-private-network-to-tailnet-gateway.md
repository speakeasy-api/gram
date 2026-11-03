---
"server": minor
"admin": minor
"dashboard": patch
---

Staff can move an organization's private network ingress from the Tailscale operator to the tailnet gateway from a new Private network page in the admin dashboard. The move first proves the saved OAuth client can register gateway nodes, then hands over to the reconciler, and the page follows the gateway nodes as they come up. The audit log names the move.
