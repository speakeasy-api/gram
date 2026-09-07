import { createFileRoute } from "@tanstack/react-router";

import { ProjectLookup } from "@/pages/ProjectLookup";

export const Route = createFileRoute("/projects/")({
  component: ProjectLookup,
});
