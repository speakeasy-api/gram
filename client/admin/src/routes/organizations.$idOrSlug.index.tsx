import { createFileRoute } from "@tanstack/react-router";

import { OverviewRoute } from "@/pages/organization/Overview";

export const Route = createFileRoute("/organizations/$idOrSlug/")({
  component: OverviewRoute,
});
