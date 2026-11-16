import { createFileRoute } from "@tanstack/react-router";

import { OnboardingRoute } from "@/pages/organization/Onboarding";

export const Route = createFileRoute("/organizations/$idOrSlug/onboarding")({
  component: OnboardingRoute,
  staticData: { crumb: "Onboarding" },
});
