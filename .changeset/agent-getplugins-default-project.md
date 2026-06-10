---
"server": patch
---

Give each published project its own device-agent marketplace instead of
collapsing an org to one.

Previously `agent.getPlugins` derived the marketplace name from the org alone, so
every project in an org computed the same name and all but one were dropped — and
which one survived depended on alphabetical project-slug order, so a multi-project
org could receive the wrong project's marketplace (its observability hooks then
reporting to the wrong project). The view also ignored the per-project name
override entirely.

Marketplace names are now project-scoped: the org's default project (its oldest,
by id ASC) keeps the bare `<org>-speakeasy` name it always had, and every other
project gets `<org>-<project>-speakeasy`. The agent resolves each name exactly the
way the publish path does — per-project override if set, else this default — so a
device now receives every marketplace the org has published, each pointing at its
own project. Names that still genuinely collide (e.g. two equal overrides) collapse
deterministically to the default project.

No schema change. Single-project orgs and every org's default project keep their
existing name, so their installs don't churn; only non-default projects get a new
name, and the automated generator rollout republishes them (their content
fingerprint changes) so the published marketplace.json matches what the agent
emits.
