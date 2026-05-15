---
"server": patch
---

assistants now reap individual stopped runtime VMs once they've been idle for 14 days, instead of waiting for the entire assistant to fall silent for a week. Busy projects no longer accumulate orphaned per-thread Fly machines, and the next event on a dormant thread cold-launches into the same Fly app — keeping its IP and secrets.
