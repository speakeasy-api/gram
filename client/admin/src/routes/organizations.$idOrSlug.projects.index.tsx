import { createFileRoute } from "@tanstack/react-router";

import { ProjectsRoute } from "@/pages/organization/Projects";

export const Route = createFileRoute("/organizations/$idOrSlug/projects/")({
  component: ProjectsRoute,
  staticData: { crumb: "Projects" },
});
