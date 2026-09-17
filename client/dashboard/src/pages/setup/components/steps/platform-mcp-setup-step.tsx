import { Button } from "@/components/ui/Button";
import { PlatformMCPOnboardingContent } from "@/pages/org/PlatformMCP";
import { RequireScope } from "@/components/require-scope";
import { AddExistingMCPServers } from "../add-existing-mcp-servers";
import { StepSupportButton } from "../step-container";

import { StepSection } from "../step-section";
import { useJourneyView } from "../journey-steps";

interface PlatformMCPSetupStepProps {
  onComplete: () => void;
  currentProjectSlug?: string;
}

export function PlatformMCPSetupStep({
  onComplete,
  currentProjectSlug,
}: PlatformMCPSetupStepProps): JSX.Element {
  const journey = useJourneyView();
  const previousStep = journey.steps.findLast(
    (step) => step.index < (journey.activeIndex ?? 1),
  );
  const nextStep = journey.steps.find(
    (step) => step.index > (journey.activeIndex ?? 1),
  );
  return (
    <div className="flex flex-col gap-6">
      <RequireScope scope="org:admin" level="page">
        <StepSection
          index={1}
          slug="set-up-platform-mcp"
          title="Set up Platform MCP"
        >
          <PlatformMCPOnboardingContent
            currentProjectSlug={currentProjectSlug}
            embeddedInProjectSetup
            onSetupComplete={onComplete}
          />
        </StepSection>
        <AddExistingMCPServers currentProjectSlug={currentProjectSlug} />
      </RequireScope>
      <div className="flex flex-wrap justify-end gap-3 border-t pt-6">
        {previousStep ? (
          <Button
            variant="tertiary"
            className="focus-visible:ring-2 focus-visible:ring-offset-3 focus-visible:ring-[var(--border-focus)] focus-visible:ring-offset-[var(--bg-surface-primary-default)]"
            onClick={() => journey.setActiveIndex(previousStep.index)}
          >
            <Button.Text>Back</Button.Text>
          </Button>
        ) : null}
        {nextStep ? (
          <Button
            variant="secondary"
            className="focus-visible:ring-2 focus-visible:ring-offset-3 focus-visible:ring-[var(--border-focus)] focus-visible:ring-offset-[var(--bg-surface-primary-default)]"
            onClick={() => journey.setActiveIndex(nextStep.index)}
          >
            <Button.Text>Next step</Button.Text>
          </Button>
        ) : null}
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
