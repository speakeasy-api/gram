import { useState } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { MarketplaceSection } from "../marketplace-section";
import { marketplaceHoldBacks } from "../marketplace-status";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isCursorSource } from "../hook-event-sources";
import { PlatformSetupFlow } from "../platform-setup-flow";
import { platformStatusBadge } from "../platform-status-badge";
import type { PlatformSetupStatus } from "../../types";

interface CursorAdminControlsStepProps {
  onComplete: () => void;
}

export function CursorAdminControlsStep({
  onComplete,
}: CursorAdminControlsStepProps): JSX.Element {
  const [status, setStatus] = useState<PlatformSetupStatus>("not_started");
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    throwOnError: false,
  });
  // The team marketplace imports the repo through GitHub, so its org rollout
  // also waits on a collaborator; the individual-plan path uses a ZIP.
  const { heldBack, orgHeldBack } = marketplaceHoldBacks(publishStatus);

  return (
    <StepContainer
      title="Set up Cursor admin controls"
      description="On Cursor Teams or Enterprise, an admin imports your plugin marketplace as a team marketplace and chooses how it installs. Skip this card if the Speakeasy device agent already manages Cursor on your team's machines: it installs the same plugin. Turn logging on, publish your plugin marketplace, connect Cursor, and verify actual event delivery."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <MarketplaceSection
          index={2}
          requiresCollaborators
          description="Cursor imports the observability plugin from your marketplace's GitHub repo."
          publishedHint="Import this repo when Cursor asks for the team marketplace repository."
        />

        <StepSection
          index={3}
          slug="connect-cursor"
          title="Connect Cursor"
          description="On Cursor Teams or Enterprise, import the repo through the team marketplace and choose an installation mode; import alone does not install the plugin for everyone. On an individual plan, each person installs a local copy. Verify hook events after setup."
          complete={status === "complete"}
          aside={platformStatusBadge(status)}
        >
          <PlatformSetupFlow
            platformId="cursor"
            status={status}
            onStatusChange={setStatus}
            heldBack={heldBack}
            orgHeldBack={orgHeldBack}
          />
        </StepSection>

        <ConfirmTrafficSection
          index={4}
          description="Run any tool in Cursor. Confirm its events actually arrive here; installation alone does not prove delivery."
          matchesSource={isCursorSource}
        />
      </div>
    </StepContainer>
  );
}
