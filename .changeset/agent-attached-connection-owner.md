---
"server": patch
---

An agent using a personal upstream connection a user attached to it is now checked against that connection's owner when upstream auth is enforced, and the authorization shadow records the check instead of skipping it. The owner comes from the same exact, tenant- and sign-in-scoped attachment that serves the token, so a revoked, deleted, reconnected or out-of-scope attachment still names nobody and is refused. Ownership only narrows the agent's grants: a connection attached by anyone other than the user the agent represents is still refused, and the token served must come from the connection the check was decided for.
