---
"dashboard": patch
---

Add three browser hardening headers to the dashboard HTML responses: `Cross-Origin-Resource-Policy: same-origin`, `Cross-Origin-Opener-Policy: same-origin`, and `X-Permitted-Cross-Domain-Policies: none`. A penetration test reported all three as missing. Each one is set per location, because an `add_header` inside an nginx location discards every `add_header` inherited from the server block. Static assets under `/assets` and `/external` keep `Access-Control-Allow-Origin: *` and receive no cross-origin policy, so cross-origin image loads continue to work.
