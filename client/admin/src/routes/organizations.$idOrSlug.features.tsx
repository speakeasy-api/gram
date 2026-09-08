import { createFileRoute } from "@tanstack/react-router";

import { FeaturesRoute } from "@/pages/organization/Features";

export const Route = createFileRoute("/organizations/$idOrSlug/features")({
  component: FeaturesRoute,
  staticData: { crumb: "Features" },
});
