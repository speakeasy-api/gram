import { createFileRoute } from "@tanstack/react-router";

import { projectQuery } from "@/lib/adminQueries";
import { ProjectDetailRoute } from "@/pages/ProjectDetail";

export const Route = createFileRoute("/projects/$idOrSlug")({
  component: ProjectDetailRoute,
  staticData: {
    crumb: ({ idOrSlug }) => (idOrSlug ? projectQuery(idOrSlug) : undefined),
  },
});
