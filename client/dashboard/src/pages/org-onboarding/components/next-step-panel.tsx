import type { OnboardingState } from "@gram/client/models/components/onboardingstate.js";
import type { OnboardingStep } from "@gram/client/models/components/onboardingstep.js";
import { ArrowUpRight, Check, Loader2, RefreshCw } from "lucide-react";
import { useEffect } from "react";
import { Link } from "react-router";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { destinationLabel, useDestinationHref } from "../use-destination-href";

/** How often the panel re-checks evidence while a step is open. */
const CHECK_INTERVAL_MS = 20_000;

export interface CheckResult {
  verified: boolean;
  evidence: string;
}

/**
 * The one next step and where it stands: what to do, where to do it, what
 * counts as proof, and the check that turns proof into progress.
 */
export function NextStepPanel({
  state,
  onCheck,
  checking,
  lastCheck,
  autoCheck = true,
}: {
  state: OnboardingState;
  onCheck: () => void;
  checking: boolean;
  lastCheck: CheckResult | null;
  /** Poll for evidence while the step is on screen. */
  autoCheck?: boolean;
}): JSX.Element {
  const hrefFor = useDestinationHref();
  const next = state.nextStep;

  useEffect(() => {
    if (!autoCheck || !next || state.done) return;
    const timer = window.setInterval(onCheck, CHECK_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [autoCheck, next, state.done, onCheck]);

  if (state.done) {
    const verified = state.steps.filter((step) => step.verifiedAt);
    return (
      <div className="flex flex-col gap-6">
        <InlineEmptyState
          icon="party-popper"
          heading="Your use case is covered"
          description="Real traffic confirmed the setup. Add more products or change the use case any time from onboarding settings."
          action={
            <Button asChild>
              <Link to={hrefFor(verified[0]?.destination ?? "settings")}>
                See it in the dashboard
              </Link>
            </Button>
          }
        />
        <StepList steps={state.steps} currentSlug={undefined} />
      </div>
    );
  }

  if (!next) {
    return (
      <Alert variant="info">
        <div>
          <AlertTitle>Nothing to set up for these answers</AlertTitle>
          <AlertDescription>
            None of the products you picked can be covered on the plans you
            have. Change the products or the use case to get a next step.
          </AlertDescription>
        </div>
      </Alert>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <section className="bg-card border-border flex flex-col gap-4 border p-6">
        <span className="text-eyebrow">Next step</span>
        <h2 className="text-foreground text-display-xs font-thin">
          {next.title}
        </h2>
        <p className="text-muted-foreground text-sm leading-relaxed">
          {next.description}
        </p>
        <p className="text-muted-foreground text-sm">
          <span className="text-foreground font-medium">
            We will look for:{" "}
          </span>
          {next.evidence}.
        </p>
        <div className="flex flex-wrap items-center gap-3">
          <Button asChild>
            <Link
              to={hrefFor(next.destination)}
              target="_blank"
              rel="noopener noreferrer"
            >
              {destinationLabel(next.destination)}
              <ArrowUpRight className="h-4 w-4" aria-hidden="true" />
            </Link>
          </Button>
          <Button variant="secondary" onClick={onCheck} disabled={checking}>
            {checking ? (
              <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
            ) : (
              <RefreshCw className="h-4 w-4" aria-hidden="true" />
            )}
            Check now
          </Button>
        </div>
        {lastCheck ? (
          <p
            role="status"
            className={cn(
              "text-sm",
              lastCheck.verified
                ? "text-default-success"
                : "text-muted-foreground",
            )}
          >
            {lastCheck.evidence}
            {lastCheck.verified ? "" : " We check again every 20 seconds."}
          </p>
        ) : null}
      </section>
      <StepList steps={state.steps} currentSlug={next.slug} />
    </div>
  );
}

function StepList({
  steps,
  currentSlug,
}: {
  steps: OnboardingStep[];
  currentSlug: string | undefined;
}): JSX.Element | null {
  if (steps.length < 2) return null;
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-eyebrow">Plan for this use case</h3>
      <ol className="flex flex-col gap-1.5">
        {steps.map((step, index) => {
          const done = Boolean(step.verifiedAt);
          const current = step.slug === currentSlug;
          return (
            <li
              key={step.slug}
              className={cn(
                "flex items-center gap-2 text-sm",
                current
                  ? "text-foreground font-medium"
                  : "text-muted-foreground",
              )}
            >
              {done ? (
                <Check
                  className="text-default-success h-3.5 w-3.5 flex-shrink-0"
                  strokeWidth={3}
                  aria-label="Verified"
                />
              ) : (
                <span
                  aria-hidden="true"
                  className="w-3.5 flex-shrink-0 text-center font-mono text-xs"
                >
                  {index + 1}
                </span>
              )}
              {step.title}
            </li>
          );
        })}
      </ol>
    </section>
  );
}
