import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

export interface Step {
  id: string;
  title: string;
  description: string;
  /** Optional inline marker after the title, e.g. "Required" / "Optional". */
  badge?: string;
}

interface OnboardingStepperProps {
  steps: Step[];
  currentStep: number;
  onStepClick?: (index: number) => void;
  maxAllowedStep?: number;
  /** Allow clicking ahead to upcoming (unlocked) steps, not just back to
   *  completed ones. Off by default to preserve the linear onboarding flow. */
  allowJumpAhead?: boolean;
}

export function OnboardingStepper({
  steps,
  currentStep,
  onStepClick,
  maxAllowedStep = steps.length - 1,
  allowJumpAhead = false,
}: OnboardingStepperProps): JSX.Element {
  return (
    <nav className="flex flex-col" aria-label="Progress">
      {steps.map((step, index) => {
        const isCompleted = index < currentStep;
        const isCurrent = index === currentStep;
        const isUpcoming = index > currentStep;
        const isLocked = index > maxAllowedStep;
        const isLast = index === steps.length - 1;
        const canJump =
          !!onStepClick &&
          !isLocked &&
          !isCurrent &&
          (isCompleted || allowJumpAhead);

        return (
          <div key={step.id} className="relative flex gap-4">
            {/* Vertical line connector - runs through all steps except last */}
            {!isLast && (
              <div
                className="absolute top-[28px] left-[13px] h-[calc(100%-28px)] w-px bg-border"
                aria-hidden="true"
              />
            )}

            {/* Step indicator */}
            <div className="relative z-10 flex-shrink-0">
              {isCurrent ? (
                /* Active step: dark filled rounded rectangle */
                <div className="flex h-[28px] w-[28px] items-center justify-center rounded-[8px] bg-foreground text-sm font-semibold text-background">
                  {index + 1}
                </div>
              ) : isCompleted ? (
                /* Completed step: dark circle with checkmark */
                <button
                  onClick={() => onStepClick?.(index)}
                  className="flex h-[28px] w-[28px] cursor-pointer items-center justify-center rounded-full bg-foreground text-background transition-all duration-200 ease-out hover:scale-[1.2] hover:bg-foreground/80"
                >
                  <Check className="h-3.5 w-3.5" strokeWidth={2.5} />
                </button>
              ) : (
                /* Upcoming step: light outlined circle with white fill to cover track */
                <div
                  className={cn(
                    "flex h-[28px] w-[28px] items-center justify-center rounded-full border bg-background text-sm font-normal",
                    isLocked
                      ? "border-border text-muted-foreground/40"
                      : "border-border text-muted-foreground",
                  )}
                >
                  {index + 1}
                </div>
              )}
            </div>

            {/* Step content */}
            <div
              className={cn("min-w-0 pt-1 pb-8", canJump && "cursor-pointer")}
              onClick={canJump ? () => onStepClick?.(index) : undefined}
            >
              <h3
                className={cn(
                  "flex items-center gap-2 text-sm leading-tight font-semibold",
                  isCurrent && "text-foreground",
                  isCompleted && "text-foreground",
                  isUpcoming && "text-muted-foreground",
                )}
              >
                {step.title}
                {step.badge && (
                  <span className="text-muted-foreground text-[10px] font-semibold tracking-wide uppercase">
                    {step.badge}
                  </span>
                )}
              </h3>
              <p
                className={cn(
                  "mt-0.5 text-sm leading-snug",
                  isCurrent && "text-muted-foreground",
                  isCompleted && "text-muted-foreground",
                  isUpcoming && "text-muted-foreground/60",
                )}
              >
                {step.description}
              </p>
            </div>
          </div>
        );
      })}
    </nav>
  );
}
