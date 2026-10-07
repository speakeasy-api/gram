import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

export interface SetupStepsStep {
  id: string;
  title: string;
}

interface SetupStepsProps<S extends SetupStepsStep> {
  steps: readonly S[];
  /** The step on screen. An id not among steps shows the first. */
  activeStepId: string;
  /** Whether the step at index can be shown; its dash is disabled if not. */
  isReachable: (index: number) => boolean;
  onStepChange: (stepId: string) => void;
  /** The body under a step's title. Every step stays mounted for the slide. */
  renderStep: (step: S, index: number) => ReactNode;
  /** Actions under the steps, such as Back and Continue. */
  footer?: ReactNode;
}

/**
 * Steps shown one at a time, sliding sideways between them, under a row of
 * dashes that tracks progress and links back to any step that can be shown.
 * With a single step there is nothing to track, so the dashes and the step
 * number are left out.
 *
 * Presentation only: which step is active, which can be reached and what the
 * footer does belong to the caller. Renders as children of a flex column
 * whose height is bounded, such as a sheet's content, so the steps take the
 * space between the dashes and the footer and scroll within it.
 */
export function SetupSteps<S extends SetupStepsStep>({
  steps,
  activeStepId,
  isReachable,
  onStepChange,
  renderStep,
  footer,
}: SetupStepsProps<S>): JSX.Element {
  const activeIndex = Math.max(
    steps.findIndex((step) => step.id === activeStepId),
    0,
  );
  const multiStep = steps.length > 1;

  return (
    <>
      {multiStep && (
        <StepProgress
          steps={steps}
          activeIndex={activeIndex}
          isReachable={isReachable}
          onPick={(index) => onStepChange(steps[index]!.id)}
        />
      )}

      <div className="relative min-h-0 flex-1 overflow-hidden">
        <div
          className="flex h-full transition-transform duration-300 ease-in-out"
          style={{ transform: `translateX(-${activeIndex * 100}%)` }}
        >
          {steps.map((step, index) => (
            <div
              key={step.id}
              // Off-screen steps stay mounted for the slide, but out of the
              // tab order and the accessibility tree.
              inert={index !== activeIndex}
              className={cn(
                "w-full shrink-0 space-y-4 overflow-y-auto px-6 pb-6",
                !multiStep && "pt-4",
              )}
            >
              {multiStep && <p className="text-eyebrow">Step {index + 1}</p>}
              <h3 className="text-foreground text-base font-medium">
                {step.title}
              </h3>
              {renderStep(step, index)}
            </div>
          ))}
        </div>
      </div>

      {footer}
    </>
  );
}

/** One dash per step; a dash is a link back to any step that can be shown. */
function StepProgress({
  steps,
  activeIndex,
  isReachable,
  onPick,
}: {
  steps: readonly SetupStepsStep[];
  activeIndex: number;
  isReachable: (index: number) => boolean;
  onPick: (index: number) => void;
}): JSX.Element {
  return (
    <div className="flex items-center gap-1.5 px-6 pt-4 pb-4">
      {steps.map((step, index) => {
        const reachable = isReachable(index);
        return (
          <button
            key={step.id}
            type="button"
            aria-current={index === activeIndex ? "step" : undefined}
            aria-label={`Step ${index + 1}: ${step.title}`}
            disabled={!reachable}
            onClick={() => onPick(index)}
            className={cn(
              "h-1 transition-all disabled:cursor-not-allowed",
              dashClass(index, activeIndex),
            )}
          />
        );
      })}
      <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
        {activeIndex + 1}/{steps.length}
      </span>
    </div>
  );
}

function dashClass(index: number, activeIndex: number): string {
  if (index === activeIndex) return "bg-foreground w-6";
  if (index < activeIndex) return "bg-foreground/40 hover:bg-foreground/60 w-4";
  return "bg-border w-4";
}
