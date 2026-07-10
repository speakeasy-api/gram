---
"server": minor
---

Register assistants as a billed model-usage source so assistant-runtime inference counts toward tokens under management and the billing page (AGE-2850). Polar ingestion and the completions credit gate already covered the source; registration scopes it into the TUM cycle snapshots and billing reads.
