import { createFileRoute } from "@tanstack/react-router";

import { projectQuery } from "@/lib/adminQueries";
import { ProjectRoute } from "@/pages/organization/Project";

export const Route = createFileRoute(
  "/organizations/$idOrSlug/projects/$projectIdOrSlug",
)({
  component: ProjectRoute,
  staticData: {
    crumb: ({ projectIdOrSlug }) =>
      projectIdOrSlug ? projectQuery(projectIdOrSlug) : undefined,
  },
});
