import { createContext, useContext, type ReactNode } from "react";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { useJourneyView } from "./journey-steps";

const StepSupportContext = createContext<(() => void) | undefined>(undefined);

export function StepSupportButton(): JSX.Element | null {
  const onSupport = useContext(StepSupportContext);

  return onSupport ? (
    <Button variant="secondary" onClick={onSupport}>
      Get support
    </Button>
  ) : null;
}

export function StepSupportProvider({
  onSupport,
  children,
}: {
  onSupport: () => void;
  children: ReactNode;
}): JSX.Element {
  return (
    <StepSupportContext.Provider value={onSupport}>
      {children}
    </StepSupportContext.Provider>
  );
}

interface StepContainerProps {
  icon: ReactNode;
  title: string;
  description: string;
  children: ReactNode;
  onContinue?: () => void;
  isLoading?: boolean;
  canContinue?: boolean;
  /**
   * Label for the final action when the card's primary action does more than
   * mark the task done (distributing servers, say). Defaults to "Mark done".
   */
  markDoneLabel?: string;
}

export function StepContainer({
  icon,
  title,
  description,
  children,
  onContinue,
  isLoading = false,
  canContinue = true,
  markDoneLabel,
}: StepContainerProps): JSX.Element {
  const journey = useJourneyView();
  const lastStep = journey.steps[journey.steps.length - 1];
  const isLastStep = !lastStep || journey.activeIndex === lastStep.index;

  const currentPosition = journey.steps.findIndex(
    (step) => step.index === journey.activeIndex,
  );
  // Below md the rail is hidden, and with it the only way back to an earlier
  // sub-step, so the footer carries one there. On wider screens the rail is
  // the affordance and a second control would just duplicate it.
  const previousStep =
    currentPosition > 0 ? journey.steps[currentPosition - 1] : undefined;

  // A card walks its own sub-steps one at a time, so the footer is Next step
  // until the last one, where the task is marked done. A card whose primary
  // action does real work (distributing servers) keeps its own label.
  const actions = isLastStep ? (
    <Button onClick={onContinue} disabled={!canContinue || isLoading}>
      {isLoading ? "Loading..." : (markDoneLabel ?? "Mark done")}
    </Button>
  ) : (
    <Button
      onClick={() => {
        const next = journey.steps[currentPosition + 1];
        if (next) journey.setActiveIndex(next.index);
      }}
      className="gap-1.5"
    >
      Next step
      <ArrowRight className="h-4 w-4" />
    </Button>
  );

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center gap-0">
        <div className="flex-shrink-0">{icon}</div>
        <h1 className="text-foreground text-display-sm font-thin">{title}</h1>
      </div>
      <p className="text-muted-foreground mt-2 text-sm">{description}</p>

      {/* Content */}
      <div className="mt-8 flex-1">{children}</div>

      {/* Divider */}
      <div className="bg-border mt-8 h-px" />

      {/* Actions */}
      <div className="mt-6 flex items-center justify-between">
        <div>
          {previousStep ? (
            <Button
              variant="tertiary"
              onClick={() => journey.setActiveIndex(previousStep.index)}
              className="text-muted-foreground hover:text-foreground gap-1.5 md:hidden"
            >
              <ArrowLeft className="h-4 w-4" />
              Back
            </Button>
          ) : null}
        </div>
        <div className="flex items-center gap-3">
          <StepSupportButton />
          {actions}
        </div>
      </div>
    </div>
  );
}
