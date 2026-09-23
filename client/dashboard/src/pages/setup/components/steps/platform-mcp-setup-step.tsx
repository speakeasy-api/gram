import { PlatformMCPOnboardingContent } from "@/pages/org/PlatformMCP";
import { RequireScope } from "@/components/require-scope";
import { AddExistingMCPServers } from "../add-existing-mcp-servers";
import { StepContainer } from "../step-container";

import { StepSection } from "../step-section";

interface PlatformMCPSetupStepProps {
  onComplete: () => void;
  currentProjectSlug?: string;
}

export function PlatformMCPSetupStep({
  onComplete,
  currentProjectSlug,
}: PlatformMCPSetupStepProps): JSX.Element {
  return (
    <StepContainer
      title="Platform MCP"
      description="Manage MCPs, Risk Policies and explore logs in your favorite agent."
      onContinue={onComplete}
    >
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
    </StepContainer>
  );
}
