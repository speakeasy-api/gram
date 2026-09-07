import { createFileRoute } from "@tanstack/react-router";

import { ActivityRoute } from "@/pages/organization/Activity";

export const Route = createFileRoute("/organizations/$idOrSlug/activity")({
  component: ActivityRoute,
  staticData: { crumb: "Activity" },
});
