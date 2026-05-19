import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

export interface Step {
  id: string;
  title: string;
  description: string;
}

interface OnboardingStepperProps {
  steps: Step[];
  currentStep: number;
  onStepClick?: (index: number) => void;
}

export function OnboardingStepper({
  steps,
  currentStep,
  onStepClick,
}: OnboardingStepperProps) {
  return (
    <nav className="flex flex-col" aria-label="Progress">
      {steps.map((step, index) => {
        const isCompleted = index < currentStep;
        const isCurrent = index === currentStep;
        const isUpcoming = index > currentStep;
        const isLast = index === steps.length - 1;

        return (
          <div key={step.id} className="relative flex gap-4">
            {/* Vertical line connector - runs through all steps except last */}
            {!isLast && (
              <div
                className="absolute top-[28px] left-[13px] h-[calc(100%-28px)] w-px bg-[#e0e0e0]"
                aria-hidden="true"
              />
            )}

            {/* Step indicator */}
            <div className="relative z-10 flex-shrink-0">
              {isCurrent ? (
                /* Active step: dark filled rounded rectangle */
                <div className="flex h-[28px] w-[28px] items-center justify-center rounded-[8px] bg-[#1a1a1a] text-sm font-semibold text-white">
                  {index + 1}
                </div>
              ) : isCompleted ? (
                /* Completed step: dark circle with checkmark */
                <button
                  onClick={() => onStepClick?.(index)}
                  className="flex h-[28px] w-[28px] cursor-pointer items-center justify-center rounded-full bg-[#1a1a1a] text-white transition-colors hover:bg-[#333]"
                >
                  <Check className="h-3.5 w-3.5" strokeWidth={2.5} />
                </button>
              ) : (
                /* Upcoming step: light outlined circle with white fill to cover track */
                <div className="flex h-[28px] w-[28px] items-center justify-center rounded-full border border-[#d9d9d9] bg-[#f7f7f5] text-sm font-normal text-[#b5b5b5]">
                  {index + 1}
                </div>
              )}
            </div>

            {/* Step content */}
            <div className="min-w-0 pt-1 pb-8">
              <h3
                className={cn(
                  "text-sm leading-tight font-semibold",
                  isCurrent && "text-[#0f0f0f]",
                  isCompleted && "text-[#0f0f0f]",
                  isUpcoming && "text-[#a3a3a3]",
                )}
              >
                {step.title}
              </h3>
              <p
                className={cn(
                  "mt-0.5 text-sm leading-snug",
                  isCurrent && "text-[#737373]",
                  isCompleted && "text-[#737373]",
                  isUpcoming && "text-[#c4c4c4]",
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
