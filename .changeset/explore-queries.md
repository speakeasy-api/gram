---
"server": minor
---

Add the `explore` service: saved queries, Explore's one server-side object. Any member can save and update; a spec is validated against the catalog on save and again on read, so a catalog change fails visibly instead of returning wrong numbers. Deleting someone else's query needs project write access. Every change is audited.
