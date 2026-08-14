import { createFileRoute } from "@tanstack/react-router";

import { ProjectDetailRoute } from "@/pages/ProjectDetail";

export const Route = createFileRoute("/projects/$idOrSlug")({
  component: ProjectDetailRoute,
});
