import type { OnboardingState } from "@gram/client/models/components/onboardingstate.js";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { useSlugs } from "@/contexts/Sdk";
import { useOrgRoutes } from "@/routes";
import { bannerDismissedStore } from "./onboarding-stores";

/**
 * "Complete setup" for organizations that predate the wizard or left it
 * early. Dismissing hides it on this browser; the sidebar entry remains.
 */
export function OnboardingBanner({
  state,
}: {
  state: OnboardingState | undefined;
}): JSX.Element {
  const { orgSlug } = useSlugs();
  const orgRoutes = useOrgRoutes();
  const next = state?.nextStep;

  const title = next ? "Finish setting up" : "Set up your organization";
  const body = next
    ? `Your next step is "${next.title}". Real traffic marks it done.`
    : "Three questions get you to the one thing to do first, verified against real traffic.";

  return (
    <section className="bg-card border-border mx-auto mt-8 flex w-full max-w-7xl flex-wrap items-center gap-4 border px-6 py-4">
      <div className="min-w-0 flex-1">
        <span className="text-eyebrow">Onboarding</span>
        <p className="text-foreground text-sm font-medium">{title}</p>
        <p className="text-muted-foreground text-sm">{body}</p>
      </div>
      <Button asChild>
        <Link to={orgRoutes.onboarding.href()}>
          {next ? "Continue" : "Start"}
          <ArrowRight className="h-4 w-4" aria-hidden="true" />
        </Link>
      </Button>
      <Button
        type="button"
        variant="tertiary"
        size="sm"
        icon="x"
        aria-label="Dismiss onboarding banner"
        onClick={() => {
          if (orgSlug) bannerDismissedStore.write(orgSlug, true);
        }}
      />
    </section>
  );
}
