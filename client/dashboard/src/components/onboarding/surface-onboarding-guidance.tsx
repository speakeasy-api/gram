import { useOnboardingUseCaseStatus } from "@gram/client/react-query/onboardingUseCaseStatus.js";
import { ArrowRight, ArrowUpRight } from "lucide-react";
import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { useNewOnboardingEnabled } from "@/hooks/useNewOnboarding";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { surfaceGuidance, type UseCaseSlug } from "./surface-guidance-copy";
import { destinationLabel, useDestinationHref } from "./use-destination-href";

/**
 * Shown on a product surface whose use case has no evidence behind it in the
 * last 30 days: the same definition of "set up" the onboarding wizard
 * verifies against. It names the next step when the admin picked this use
 * case, and otherwise sends them into onboarding with it chosen. Renders
 * nothing while the new onboarding flag is off, while status is loading, or
 * once the surface has evidence, so the page's own empty state still applies.
 */
export function SurfaceOnboardingGuidance({
  useCase,
  className,
}: {
  useCase: UseCaseSlug;
  className?: string;
}): JSX.Element | null {
  const enabled = useNewOnboardingEnabled();
  const { hasScope } = useRBAC();
  const orgRoutes = useOrgRoutes();
  const hrefFor = useDestinationHref();
  const statuses = useOnboardingUseCaseStatus(undefined, undefined, {
    enabled,
  });

  if (!enabled || !statuses.isSuccess) return null;
  const status = statuses.data.statuses.find(
    (candidate) => candidate.useCase === useCase,
  );
  if (!status || status.verified) return null;

  const isAdmin = hasScope("org:admin");
  const guidance = surfaceGuidance(useCase, status);
  const onboardingHref = `${orgRoutes.onboarding.href()}?useCase=${useCase}`;

  return (
    <section
      aria-label="Onboarding guidance"
      className={cn(
        "bg-card border-border flex flex-wrap items-center gap-4 border px-6 py-5",
        className,
      )}
    >
      <div className="min-w-0 flex-1">
        <span className="text-eyebrow">Not set up</span>
        <p className="text-foreground text-sm font-medium">
          {guidance.heading}
        </p>
        <p className="text-muted-foreground text-sm">{guidance.description}</p>
        {!isAdmin ? (
          <p className="text-muted-foreground mt-1 text-sm">
            Ask an organization admin to finish onboarding.
          </p>
        ) : null}
      </div>
      {isAdmin && guidance.action === "step" && status.nextStep ? (
        <>
          <Button asChild>
            <Link to={hrefFor(status.nextStep.destination)}>
              {destinationLabel(status.nextStep.destination)}
              <ArrowUpRight className="h-4 w-4" aria-hidden="true" />
            </Link>
          </Button>
          <Button asChild variant="secondary">
            <Link to={orgRoutes.onboarding.href()}>Open onboarding</Link>
          </Button>
        </>
      ) : null}
      {isAdmin && guidance.action === "onboarding" ? (
        <Button asChild>
          <Link to={onboardingHref}>
            Set up in onboarding
            <ArrowRight className="h-4 w-4" aria-hidden="true" />
          </Link>
        </Button>
      ) : null}
    </section>
  );
}
