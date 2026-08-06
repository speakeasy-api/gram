---
"server": patch
---

Classify idle-timeout terminations of proxied MCP SSE streams correctly. The standalone GET listen stream ending on the proxy's 60s idle bound is now a clean close (200, no error log) instead of a 500 "unexpected error" — clients reconnect per spec, so quiet upstreams no longer produce one spurious 5xx per minute per connected client. A POST response stream going idle mid-reply now returns a 502 gateway error naming the idle bound instead of a bare context cancellation. Access logs also no longer relabel an already-committed response's status when a late error-path WriteHeader fires.
