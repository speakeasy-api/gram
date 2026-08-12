import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Check } from "lucide-react";
import type { ReactNode } from "react";

export interface WizardStep {
  id: string;
  title: string;
  description?: string;
}

/**
 * WizardPage — the chrome for a multi-step flow: a left stepper rail and a
 * right step body. Rendered full-screen (outside the main app layout, like
 * onboarding/setup). State is controlled by the caller — pass `currentStepId`
 * and render the active step as `children` (use the existing `StepContainer`
 * for the per-step back/skip/continue footer). This removes the per-wizard
 * re-implementation of the rail + progress markers.
 *
 *   <WizardPage steps={steps} currentStepId={stepId}>
 *     <StepContainer icon={…} title={…} onContinue={…}>{stepBody}</StepContainer>
 *   </WizardPage>
 */
export function WizardPage({
  steps,
  currentStepId,
  onStepSelect,
  children,
  header,
}: {
  steps: WizardStep[];
  currentStepId: string;
  /** Allow jumping to an already-visited step from the rail. */
  onStepSelect?: (id: string) => void;
  children: ReactNode;
  /** Optional brand/header above the two columns. */
  header?: ReactNode;
}): JSX.Element {
  const currentIndex = steps.findIndex((step) => step.id === currentStepId);

  return (
    <main className="bg-background flex min-h-screen w-full flex-col p-8">
      {header}
      <div className="mx-auto flex w-full max-w-5xl flex-1 gap-12 pt-8">
        <ol className="hidden w-64 shrink-0 flex-col gap-1 md:flex">
          {steps.map((step, index) => {
            const done = index < currentIndex;
            const active = step.id === currentStepId;
            return (
              <li key={step.id}>
                <button
                  type="button"
                  aria-current={active ? "step" : undefined}
                  disabled={!done || onStepSelect == null}
                  onClick={() => onStepSelect?.(step.id)}
                  className={cn(
                    "flex w-full items-start gap-3 border-l-2 px-4 py-3 text-left",
                    active
                      ? "border-primary"
                      : "border-transparent hover:border-border",
                    !done && !active && "opacity-60",
                  )}
                >
                  <span
                    className={cn(
                      "mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center border text-xs",
                      active && "border-primary text-primary",
                      done &&
                        "bg-primary text-primary-foreground border-primary",
                    )}
                  >
                    {done ? <Check className="h-3 w-3" /> : index + 1}
                  </span>
                  <span className="flex min-w-0 flex-col">
                    <Text
                      small
                      className={cn("font-medium", active && "text-foreground")}
                    >
                      {step.title}
                    </Text>
                    {step.description != null && (
                      <Text small muted className="text-xs">
                        {step.description}
                      </Text>
                    )}
                  </span>
                </button>
              </li>
            );
          })}
        </ol>
        <div className="min-w-0 flex-1">
          {currentIndex >= 0 && (
            <div className="mb-6 md:hidden">
              <Heading variant="h5" className="font-medium">
                {steps[currentIndex]?.title}
              </Heading>
            </div>
          )}
          {children}
        </div>
      </div>
    </main>
  );
}
