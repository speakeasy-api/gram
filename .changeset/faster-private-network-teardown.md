---
"server": patch
---

Removing a private network connection hosted by the Tailscale Kubernetes operator now finishes in about a minute or two instead of seven or more. Cleanup re-checks pending Kubernetes deletions every few seconds instead of backing off to two minutes between checks.
