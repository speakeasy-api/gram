import { createFileRoute } from "@tanstack/react-router";

import { FleetHealth } from "@/pages/fleet-health/FleetHealth";

export const Route = createFileRoute("/fleet-health")({
  component: FleetHealth,
  staticData: { crumb: "Fleet health" },
});
