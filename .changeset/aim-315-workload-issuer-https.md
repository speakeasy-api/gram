---
"server": patch
---

Require https for workload issuer identifiers and their `jwks_uri`. An assertion naming a plain-http issuer is untrusted, and an issuer row with a plain-http `jwks_uri` is refused before any key set fetch.
