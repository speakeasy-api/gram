---
"server": patch
---

Fix `agent.getPlugins` returning the wrong marketplace for orgs with multiple
published projects, and make it honor per-project marketplace names.

The endpoint collapses projects that share a marketplace name into one and kept
the first row's token, but the query ordered by project slug — so a multi-project
org received whichever project sorted first alphabetically (e.g. `adam` instead
of the org's default project), and the device's observability hooks then reported
to that wrong project. The query now orders by project id, so a same-named
collapse resolves to the org's default project (first by id ASC, created at org
setup) rather than an arbitrary alphabetical winner.

The view also now resolves each project's marketplace name the way the publish
path does — the per-project override (`project_marketplace_settings`) when set,
else the org-derived default — instead of always recomputing the org default.
Projects published under distinct names now surface as distinct marketplaces
rather than collapsing, and the agent stops emitting the org default for a
project that published under an override name (which the device never installed).
