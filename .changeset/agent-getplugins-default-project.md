---
"server": patch
---

Fix `agent.getPlugins` returning the wrong project's marketplace for orgs with
multiple published projects. The endpoint collapses an org's projects to a
single marketplace and kept the first row's token, but the query ordered by
project slug — so a multi-project org received whichever project sorted first
alphabetically (e.g. `adam` instead of the org's default project), and the
device's observability hooks then reported to that wrong project. The query now
orders by project id, so the collapse resolves to the org's default project
(first by id ASC, created at org setup) rather than an arbitrary alphabetical
winner.
