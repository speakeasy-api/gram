---
"server": minor
"dashboard": patch
---

Show the currently live (published) plugin version on the plugin detail page.
`getPublishStatus` now reports `live_version` — the version stamped into the
published plugin.json manifests, read back from the marketplace repo via a
single Contents API call and cached briefly — and the dashboard displays it
next to the publish freshness indicator, so it can be compared directly
against the version plugin clients like Claude Code report for installed
plugins when debugging sync lag.
