---
"server": patch
---

Report devices appearing in an organization's fleet as `gram_activity`. The MDM upsert now reports whether it inserted, which is the only way to tell a first sighting from a re-sighting, and a config's first successful sync is treated as a backfill rather than a stream of new devices.
