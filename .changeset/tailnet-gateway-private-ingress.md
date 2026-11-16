---
"server": minor
---

Private network ingresses can now be hosted by the tsnet-based tailnet gateway instead of the Tailscale Kubernetes operator. The `networkIngress.create` hostname is optional and defaults to `speakeasy`, so new ingresses are reachable at `speakeasy.<tailnet>.ts.net`.
