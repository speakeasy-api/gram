---
"server": minor
"dashboard": minor
---

Add a `source_slugs` filter to the `listDeployments` API. The standalone `/deployments` page now exposes a source multi-select so you can narrow the list to deployments that include a given OpenAPI document or Function source (OR semantics across selected slugs). The Deployments tab on a source's overview page is also scoped to that source instead of showing every workspace deployment.
