import { useMemo } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { getCoworkTeamSteps } from "../../setup-data";
import { MarketplaceSection } from "../marketplace-section";
import { marketplaceHoldBacks } from "../marketplace-status";
import { PlatformStepList } from "../platform-step-list";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";

interface ClaudePluginRegistrationStepProps {
  onComplete: () => void;
}

export function ClaudePluginRegistrationStep({
  onComplete,
}: ClaudePluginRegistrationStepProps): JSX.Element {
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    throwOnError: false,
  });
  // Claude.ai syncs the repo through its own GitHub App, so the steps wait on a
  // published marketplace that a collaborator can grant access to.
  const { heldBack, orgHeldBack } = marketplaceHoldBacks(publishStatus);
  const { registration } = useMemo(() => getCoworkTeamSteps(), []);

  return (
    <StepContainer
      title="Register Speakeasy's plugins in Claude"
      description="Claude Desktop runs only plugins registered on your Claude organization. A copy the device agent installs on a laptop isn't used, so an Owner or Primary Owner registers Speakeasy's marketplace in Claude.ai. This needs a Claude Team or Enterprise plan."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <MarketplaceSection
          index={1}
          requiresCollaborators
          description="Claude.ai syncs Speakeasy's plugins from your marketplace's GitHub repo."
          publishedHint="Select this repo when Claude.ai asks which repository to sync."
        />

        <StepSection
          index={2}
          slug="register-plugins"
          title="Register the plugins in Claude.ai"
          description="Sync the marketplace, mark the observability plugin Required, choose how your MCP plugins install, and allow Speakeasy through Claude's network egress settings."
        >
          <PlatformStepList
            platformId="claude-cowork"
            steps={registration}
            heldBack={heldBack ?? orgHeldBack}
          />
        </StepSection>
      </div>
    </StepContainer>
  );
}
