import { Button } from "@/components/ui/Button";
import { PlatformMCPOnboardingContent } from "@/pages/org/PlatformMCP";
import { RequireScope } from "@/components/require-scope";
import { StepSupportButton } from "../step-container";

interface PlatformMCPSetupStepProps {
  onComplete: () => void;
  currentProjectSlug?: string;
}

export function PlatformMCPSetupStep({
  onComplete,
  currentProjectSlug,
}: PlatformMCPSetupStepProps): JSX.Element {
  return (
    <div className="flex flex-col gap-6">
      <RequireScope scope="org:admin" level="page">
        <PlatformMCPOnboardingContent
          currentProjectSlug={currentProjectSlug}
          embeddedInProjectSetup
          onSetupComplete={onComplete}
        />
      </RequireScope>
      <div className="flex justify-end border-t pt-6">
        <div className="flex gap-3">
          <StepSupportButton />
          <Button variant="secondary" onClick={onComplete}>
            Mark done
          </Button>
        </div>
      </div>
    </div>
  );
}
