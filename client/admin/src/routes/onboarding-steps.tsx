import { createFileRoute } from "@tanstack/react-router";
import { OnboardingSteps } from "@/pages/onboarding/Steps";

export const Route = createFileRoute("/onboarding-steps")({
  component: OnboardingSteps,
  staticData: { crumb: "Steps" },
});
