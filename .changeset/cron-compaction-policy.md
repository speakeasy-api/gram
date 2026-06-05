---
"server": minor
---

Assistant cron threads now compact on every turn end and persist the compacted transcript as a new generation in `chat_messages`. The inter-fire gap (≥24h) meant the in-runner 80% context-window threshold rarely tripped before the chat ended, so history grew unbounded — one cron chat reached 795k of its 800k trigger across six generations without ever compacting. Compaction policy is now picked server-side per thread source kind (cron → on-turn-end, everything else → 60% threshold, tightened from 80%) and the runner POSTs the compacted shape to a new `/rpc/assistants.recordCompactedGeneration` endpoint so the next cold cron bootstrap loads the shorter history instead of re-reading the un-compacted prior generation.
