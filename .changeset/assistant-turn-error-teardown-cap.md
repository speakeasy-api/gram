---
"server": patch
---

fix(assistants): stop a single bad assistant turn from tearing down and recreating its runtime forever. Errors returned by a live runtime are now treated as terminal (and capped) instead of being mistaken for a dead machine, and a hard ceiling fails an event after repeated teardowns so a stuck event can no longer churn machines indefinitely.
