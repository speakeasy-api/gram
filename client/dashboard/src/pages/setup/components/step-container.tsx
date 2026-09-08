import { createContext, useContext, type ReactNode } from "react";
import { ArrowRight, ArrowLeft } from "lucide-react";
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
  onBack?: () => void;
  onSkip?: () => void;
  continueLabel?: string;
  skipLabel?: string;
  showBack?: boolean;
  isLoading?: boolean;
  canContinue?: boolean;
  /**
   * Label for the task page's final action when the card's primary action
   * does more than mark the task done (distributing servers, say). Defaults
   * to "Mark done".
   */
  markDoneLabel?: string;
}

export function StepContainer({
  icon,
  title,
  description,
  children,
  onContinue,
  onBack,
  onSkip,
  continueLabel = "Continue",
  skipLabel = "Skip",
  showBack = false,
  isLoading = false,
  canContinue = true,
  markDoneLabel,
}: StepContainerProps): JSX.Element {
  const journey = useJourneyView();
  const { onTaskPage } = journey;
  const lastStep = journey.steps[journey.steps.length - 1];
  const isLastStep = !lastStep || journey.activeIndex === lastStep.index;

  // On a task page the card walks its own sub-steps one at a time, so the
  // footer becomes Next step until the last one, where the task is marked
  // done. A card whose primary action does real work (distributing servers)
  // keeps its own label. There is no Back: the rail jumps anywhere, and the
  // board is one click away. Skip is dropped for the same reason.
  let actions: ReactNode;
  if (onTaskPage && !isLastStep) {
    actions = (
      <Button
        onClick={() => {
          const current = journey.steps.findIndex(
            (step) => step.index === journey.activeIndex,
          );
          const next = journey.steps[current + 1];
          if (next) journey.setActiveIndex(next.index);
        }}
        className="gap-1.5"
      >
        Next step
        <ArrowRight className="h-4 w-4" />
      </Button>
    );
  } else if (onTaskPage) {
    actions = (
      <Button onClick={onContinue} disabled={!canContinue || isLoading}>
        {isLoading ? "Loading..." : (markDoneLabel ?? "Mark done")}
      </Button>
    );
  } else {
    actions = (
      <Button
        onClick={onContinue}
        disabled={!canContinue || isLoading}
        className="gap-1.5"
      >
        {isLoading ? "Loading..." : continueLabel}
        {!isLoading && <ArrowRight className="h-4 w-4" />}
      </Button>
    );
  }

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
          {showBack && !onTaskPage && (
            <Button
              variant="tertiary"
              onClick={onBack}
              className="text-muted-foreground hover:text-foreground gap-1.5"
            >
              <ArrowLeft className="h-4 w-4" />
              Back
            </Button>
          )}
        </div>
        <div className="flex items-center gap-3">
          {onSkip && !onTaskPage && (
            <Button
              variant="tertiary"
              onClick={onSkip}
              className="text-muted-foreground hover:text-foreground"
            >
              {skipLabel}
            </Button>
          )}
          <StepSupportButton />
          {actions}
        </div>
      </div>
    </div>
  );
}
