---
"server": minor
"dashboard": minor
---

Auto-provision an org and attach the free-tier Polar subscription when an unauthenticated user lands on Gram with `?disposition=assistants` and has no org after IDP signin. Generates a legible random org name (e.g. `Swift Otter 42`), eagerly materializes the default project and environment, marks the org as whitelisted so it bypasses the BookDemo gate, and redirects to `/<org>/projects/default/assistants` so the credit benefit is in place before the user reaches the assistants page.
