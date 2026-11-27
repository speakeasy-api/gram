---
"server": patch
"tunnel": patch
---

Removes the Redis instance switch now that the Auth and Operations roles have moved to their own instances. Every role, the tunnel gateway's route store, the enforcement reply inbox and the pystreams reply writer use an ordinary client of the role's configured endpoint, and the `*_NEXT_*` settings are no longer read. The role guard no longer treats the switch's own keys as belonging to every role.
