---
"server": patch
"dashboard": patch
---

OTEL forwarding updates now preserve encrypted values for unchanged headers while still treating the submitted header list as the complete desired set. Adding or removing one header no longer clears the values of retained headers.
