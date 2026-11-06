---
"dashboard": patch
---

An agent's Permissions tab now says in plain words which upstream credentials each `mcp:auth` grant covers, for example "Every upstream credential in the organization", "Content calendar · Service account", "Personal connections of the person the agent acts for" or "Ada Lovelace's Grain connection". It used to show "All resources" for every grant on all credentials, even when the grant was narrowed to one person's connection or to the agent's own user, and a raw id for a single credential. The `mcp:auth` heading also gets a description.
