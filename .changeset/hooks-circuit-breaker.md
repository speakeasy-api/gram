---
"server": patch
---

fix(hooks): keep Claude coding sessions usable when Gram is unreachable. When Observability Mode is off, generated Claude hook scripts now run optimistically — the server stays the sole authority on allow/block while reachable (2xx allows, 4xx blocks), but connection failures and 5xx responses fail open with degradation context instead of wedging the tool call. A new filesystem-backed circuit breaker (`hooks/breaker.sh`) counts outages within an error window, opens after a threshold to skip further calls, and periodically probes for recovery via a half-open lock. Bumps the plugin generator version to 5.
