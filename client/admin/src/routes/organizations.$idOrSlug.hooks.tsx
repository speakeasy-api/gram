import { createFileRoute } from "@tanstack/react-router";

import { HookFleetRoute } from "@/pages/organization/HookFleet";

export const Route = createFileRoute("/organizations/$idOrSlug/hooks")({
  component: HookFleetRoute,
  staticData: { crumb: "Hooks" },
});
