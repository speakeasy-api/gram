---
"server": patch
---

Admission for the workload assertion grant: resolve an assertion's issuer against the endpoint's trusted issuers before anything is fetched, share one issuer resolution between the management API and admission, and remember recent rejections briefly so a repeated unknown issuer costs no query
