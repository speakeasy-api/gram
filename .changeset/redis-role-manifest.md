---
"server": patch
---

Every Redis key family the server writes is declared in one manifest with its role: auth (sessions, sign-in and OAuth state, auth rate limits), ops (queues, routes, hooks, dedupe, locks and other rate limits) or evictable (caches that rebuild on a miss). A role guard and role-checking cache adapters refuse keys of another role, or keys no family declares, in enforce mode and count them in report mode; an undeclared key is counted with the `unclassified` label and is never treated as auth state. Tests now fail when they write a key the manifest does not declare. Nothing uses the roles yet, so runtime behaviour is unchanged.
