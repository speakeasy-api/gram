import { createFileRoute } from "@tanstack/react-router";

import { ProjectDetail } from "@/pages/ProjectDetail";

export const Route = createFileRoute("/projects/$idOrSlug")({
  component: ProjectDetail,
});
