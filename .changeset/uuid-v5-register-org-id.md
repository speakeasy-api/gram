---
"server": patch
---

Derive org IDs as deterministic UUIDv5 from WorkOS org ID during Register and auto-provisioning, replacing the previous `"org_" + random UUID` format which was not a valid UUID.
