import { createFileRoute } from "@tanstack/react-router";

import { CoverageRoute } from "@/pages/organization/Coverage";

export const Route = createFileRoute("/organizations/$idOrSlug/coverage")({
  component: CoverageRoute,
  staticData: { crumb: "Coverage" },
});
