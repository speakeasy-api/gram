import { createFileRoute } from "@tanstack/react-router";
import { OnboardingPlaybooksRoute } from "@/pages/onboarding/Playbooks";
import { playbooksSearch } from "@/pages/onboarding/playbooksSearch";

export const Route = createFileRoute("/onboarding-playbooks")({
  component: OnboardingPlaybooksRoute,
  validateSearch: playbooksSearch,
  staticData: { crumb: "Use Cases & Playbooks" },
});
