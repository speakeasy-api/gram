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
          // The whole row is the single interactive control for the step. The
          // indicator used to be a nested <button> alongside a role="button"
          // content div, which gave completed steps two tab stops for one
          // action; the indicator is now presentational and the row owns the
          // click, keyboard handling, and accessible name (its title + text).
          <div
            key={step.id}
            className={cn(
              "group relative flex gap-4",
              canJump && "cursor-pointer",
            )}
            aria-current={isCurrent ? "step" : undefined}
            role={canJump ? "button" : undefined}
            tabIndex={canJump ? 0 : undefined}
            onClick={canJump ? () => onStepClick?.(index) : undefined}
            onKeyDown={
              canJump
                ? (e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onStepClick?.(index);
                    }
                  }
                : undefined
            }
          >
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
                /* Active step: dark filled square */
                <div className="flex h-[28px] w-[28px] items-center justify-center bg-foreground text-sm font-semibold text-background">
                  {index + 1}
                </div>
              ) : isCompleted ? (
                /* Completed step: outlined square with a bold green checkmark;
                   active keeps the solid ink fill, upcoming stays outlined */
                <div
                  aria-hidden="true"
                  className="border-border text-default-success flex h-[28px] w-[28px] items-center justify-center border bg-background transition-all duration-200 ease-out group-hover:scale-[1.2] group-hover:border-foreground"
                >
                  <Check className="h-4 w-4" strokeWidth={3} />
                </div>
              ) : canJump ? (
                /* Upcoming but jumpable: outlined square. Presentational —
                   the row wrapper carries the click and the accessible name. */
                <div
                  aria-hidden="true"
                  className="border-border text-muted-foreground flex h-[28px] w-[28px] items-center justify-center border bg-background text-sm font-normal transition-all duration-200 ease-out group-hover:scale-[1.2] group-hover:text-foreground"
                >
                  {index + 1}
                </div>
              ) : (
                /* Upcoming step: light outlined square with white fill to cover track */
                <div
                  className={cn(
                    "flex h-[28px] w-[28px] items-center justify-center border bg-background text-sm font-normal",
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
            <div className="min-w-0 pt-1 pb-8">
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
