---
"server": patch
---

Assert worker schedule registration against the schedules the Temporal client created rather than a hand-maintained count polled from the eventually consistent schedule listing, so a new schedule can no longer leave the test passing on some runs and failing on others.
