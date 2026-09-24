---
"server": patch
---

Refuse an ambiguous issuer within a tier when admitting a workload. Tier precedence resolves across tiers — a project row beats an organization one, as the verification path does — but two rows sharing an issuer URL at the same tier are refused rather than silently resolved, because choosing between them would decide which `jwks_uri` verifies the subject and whether wildcards are permitted for it. The issuer and JWKS attributes also carry `FormatURI`, so a value that is not a URI is rejected by generated clients and by the contract.
