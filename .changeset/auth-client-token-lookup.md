---
"server": patch
---

When the auth client resolver is enforced, a personal connection or service account resolves through its OAuth registration without requiring a link to the server's sign-in, so one consent serves every server the registration reaches: a project registration's servers in its project, and an organization or global registration's servers across the organization the consent was made in, never another organization.

With the flags off, lookups are unchanged. A re-consent through another organization's sign-in now replaces the stored grant with a new one recorded in that organization, rather than updating the old grant in place. Workloads, agents and other automated callers in the first organization then need the user to reconnect there, instead of silently using a token consented in the other organization.
