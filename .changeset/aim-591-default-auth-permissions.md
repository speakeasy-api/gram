---
"server": patch
"admin": patch
"dashboard": patch
---

Organizations can now be granted the default auth permissions the mandatory auth client check needs: everyone's and each agent's use of their own personal connections, shared clients for whoever can connect to a server using them, and a policy for each legacy API key that has none. An offline backfill grants them for every organization and records each grant it writes, so a rerun never restores one an admin removed; the upstream OAuth migration grants them for the clients it records, and new organizations start with the personal default. Role permission counts no longer include these provisioned permissions.
