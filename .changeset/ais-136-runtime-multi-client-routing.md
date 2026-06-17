---
"server": patch
---

Resolve multiple remote-session authorizations per user session issuer at the
MCP runtime, keyed by remote session issuer, and enforce at most one client per
(user session issuer, remote session issuer) at attach time. The runtime
resolves a per-issuer token map and re-auths when any attached remote session
is missing or invalid; an application-level attach guard plus a runtime
invariant replace the database one_per_issuer index. Issuer-gated dispatch
fails closed when it cannot route among multiple upstream tokens.
