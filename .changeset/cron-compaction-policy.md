---
"server": minor
---

Assistant cron threads now compact on every turn end. Their inter-fire gap (≥24h) meant the 80% context-window threshold rarely tripped before the chat ended, so history grew unbounded — one cron chat reached 795k of its 800k trigger across six generations without ever compacting. Compaction policy is now picked server-side per thread source kind: cron → on-turn-end, everything else → 60% threshold (tightened from 80%). The runner is policy-agnostic and dispatches on a sealed variant in the thread bootstrap.
