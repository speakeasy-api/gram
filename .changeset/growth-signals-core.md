---
"server": patch
---

Add the `growthsignals` package, which describes notable moments in Gram — organizations and projects created, MCP servers deployed, security policies written, members invited and joining — as a single PostHog `gram_activity` event with a stable property shape. It carries the activity taxonomy, the map from audit action to activity (including the pass-through name that gives uncurated actions coverage and the exclusion list that keeps high-volume noise out), the event builder, a repo-backed enricher behind a TTL cache, and the emitter that skips the demo organization and logs rather than returns capture failures. Nothing calls it yet, so no events are emitted and no behaviour changes.
