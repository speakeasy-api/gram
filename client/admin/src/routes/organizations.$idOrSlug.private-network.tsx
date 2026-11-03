import { createFileRoute } from "@tanstack/react-router";

import { PrivateNetworkRoute } from "@/pages/organization/PrivateNetwork";

export const Route = createFileRoute(
  "/organizations/$idOrSlug/private-network",
)({
  component: PrivateNetworkRoute,
  staticData: { crumb: "Private network" },
});
