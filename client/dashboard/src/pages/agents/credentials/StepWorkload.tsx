import type { SetupStep } from "./setupInstructions";

import type { JSX } from "react";
import { WizardStepHeader } from "./WizardChrome";
import type { WorkloadSetup } from "./useWorkloadSetup";

/** One step of a platform's setup, as a step of this wizard. */
export function StepWorkload({
  step,
  setup,
  first,
  platformName,
}: {
  step: SetupStep;
  setup: WorkloadSetup;
  /** Whether this is the platform's first step, which carries what it needs. */
  first: boolean;
  platformName: string;
}): JSX.Element {
  return (
    <div className="space-y-6">
      <WizardStepHeader title={step.title} />
      {setup.error !== null && (
        <p
          role="alert"
          className="border-destructive text-destructive border p-3 text-sm"
        >
          {setup.error}
        </p>
      )}
      <div className="space-y-4">{setup.render(step)}</div>
      {/* A precondition, not an instruction: it sits under the work, folded
          away, because the reader who already has it must not be stopped by
          it on the way in. */}
      {first && setup.requirements !== null && (
        <details>
          <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
            What you need for {platformName}
          </summary>
          <div className="bg-muted/40 border-muted-foreground/30 mt-2 border p-4">
            {setup.requirements}
          </div>
        </details>
      )}
    </div>
  );
}
