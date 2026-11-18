---
"server": patch
---

Removing a private network connection hosted by the Tailscale Kubernetes operator no longer fails a cleanup attempt with a Kubernetes `422 Invalid` error when its OAuth Secret is deleted, which added a retry to most teardowns.
