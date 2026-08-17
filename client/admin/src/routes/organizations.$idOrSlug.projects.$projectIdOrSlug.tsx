import { createFileRoute } from "@tanstack/react-router";

import { projectQuery } from "@/lib/adminQueries";
import { ProjectRoute } from "@/pages/organization/Project";

export const Route = createFileRoute(
  "/organizations/$idOrSlug/projects/$projectIdOrSlug",
)({
  component: ProjectRoute,
  staticData: {
    // Both params, so this key matches the one the view under the bar fetches.
    // The bar only watches the cache, so a key that differs by the organization
    // would never fill.
    crumb: ({ projectIdOrSlug, idOrSlug }) =>
      projectIdOrSlug ? projectQuery(projectIdOrSlug, idOrSlug) : undefined,
  },
});
