---
"dashboard": patch
---

Reloading the new-agent wizard past its first step now starts the draft over at the name step. The draft is kept only in the page, so a reload left the install step with no name and no key, and "Issue a key" failed with `length of body.name must be greater or equal than 1`.
