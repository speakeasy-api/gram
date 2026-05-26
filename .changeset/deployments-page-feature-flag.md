---
"dashboard": patch
---

Gate the top-level Deployments nav item behind the `gram-deployments-page` PostHog feature flag. Defaults to true (visible to all orgs); flip off for specific orgs via PostHog org-group targeting (`organization_slug`). The page route itself is not gated — direct URLs and cross-page links to `/deployments` continue to work.
