---
"server": patch
---

Add a `workload` kind to the session subject URN, so a machine vouched for by an external issuer can be the subject of a Gram-issued session. The identity carries the issuer alongside the subject it asserted, since a `sub` is unique within an issuer and never across one
