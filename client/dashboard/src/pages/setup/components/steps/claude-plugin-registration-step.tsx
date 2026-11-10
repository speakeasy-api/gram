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
      description="Claude Desktop only runs plugins registered in your Claude organization. A copy the device agent installs isn't used. Needs an Owner or Primary Owner on Team or Enterprise."
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
          description="Sync the marketplace, set plugin access, and allow Speakeasy's domains."
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
