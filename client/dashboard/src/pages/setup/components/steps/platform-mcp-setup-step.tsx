import { Button } from "@/components/ui/Button";
import { PlatformMCPOnboardingContent } from "@/pages/org/PlatformMCP";
import { RequireScope } from "@/components/require-scope";

interface PlatformMCPSetupStepProps {
  onComplete: () => void;
  onBack: () => void;
  onSkip: () => void;
  currentProjectSlug?: string;
  continueLabel: string;
}

export function PlatformMCPSetupStep({
  onComplete,
  onBack,
  onSkip,
  currentProjectSlug,
  continueLabel,
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
      <div className="flex justify-between border-t pt-6">
        <Button variant="tertiary" onClick={onBack}>
          Back
        </Button>
        <div className="flex gap-3">
          <Button variant="tertiary" onClick={onSkip}>
            Skip for now
          </Button>
          <Button variant="secondary" onClick={onComplete}>
            {continueLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
