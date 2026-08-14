import { createFileRoute } from "@tanstack/react-router";

import { ProjectRoute } from "@/pages/organization/Project";

export const Route = createFileRoute(
  "/organizations/$idOrSlug/projects/$projectIdOrSlug",
)({
  component: ProjectRoute,
});
