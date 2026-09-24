---
"dashboard": patch
---

Keep the Workload Identities page usable when agents cannot be listed. The agent management API returns `404` for an organization without that rollout, and the dashboard's global query policy suppresses only `401` and `403`, so the agent lookup threw to the error boundary and took down a page whose own data had loaded. Listing and withdrawing need no agent; only admitting does, so the page now renders the trust policy and disables the admit action with the reason it is unavailable.
