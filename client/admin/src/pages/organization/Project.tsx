import type { JSX } from "react";
import { useParams } from "@tanstack/react-router";

import { ProjectDetail } from "@/pages/ProjectDetail";

// The record's own project view. It draws the same page the global project
// route draws, and it can scope the read, because this URL names the
// organization as well.
export function ProjectRoute(): JSX.Element {
  const { projectIdOrSlug, idOrSlug } = useParams({
    from: "/organizations/$idOrSlug/projects/$projectIdOrSlug",
  });
  return (
    <ProjectDetail idOrSlug={projectIdOrSlug} organizationIdOrSlug={idOrSlug} />
  );
}
