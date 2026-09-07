import { useState } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { AGENT_PROVIDERS } from "@/components/agent-providers/agent-providers";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { MarketplaceSection } from "../marketplace-section";
import { isMarketplacePublished } from "../marketplace-status";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isCoworkSource } from "../hook-event-sources";
import { AgentPlatformPickerItem } from "../agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "../platform-instrumentation-sheet";
import { platformStatusBadge } from "../platform-status-badge";
import { ANTHROPIC_ENTERPRISE_PLATFORM_ID } from "../../setup-data";
import type { PlatformSetupStatus } from "../../types";

interface AnthropicEnterpriseStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function AnthropicEnterpriseStep({
  onComplete,
  onBack,
}: AnthropicEnterpriseStepProps): JSX.Element {
  const [sheetOpen, setSheetOpen] = useState(false);
  const [status, setStatus] = useState<PlatformSetupStatus>("not_started");
  const { data: publishStatus } = usePublishStatus();
  const published = isMarketplacePublished(publishStatus);
  const provider = AGENT_PROVIDERS[ANTHROPIC_ENTERPRISE_PLATFORM_ID];

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <AgentProviderIcon source="claude" className="h-6 w-6" />
        </div>
      }
      title="Set up Anthropic Enterprise"
      description="Claude Cowork runs in Claude.ai's cloud sandbox, out of the device agent's reach, so it is configured from Claude.ai's organization settings instead. Publish your plugin marketplace, register it there with the observability plugin required, and confirm Cowork events arrive. Claude Code is instrumented alongside your other coding assistants under Instrument agents."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-8">
        <MarketplaceSection
          index={1}
          description="Claude.ai syncs Cowork plugins straight from your marketplace's GitHub repo, so it has to exist before you can register it."
          publishedHint="Select this repo when Claude.ai asks which repository to sync."
        />

        <StepSection
          index={2}
          title="Connect Claude Cowork"
          description="Register the marketplace in Claude.ai's organization settings and mark the observability plugin as required for every member."
          complete={status === "complete"}
          aside={platformStatusBadge(status)}
        >
          <AgentPlatformPickerItem
            platformId={ANTHROPIC_ENTERPRISE_PLATFORM_ID}
            name={provider.name}
            description={provider.description}
            complete={status === "complete"}
            disabled={!published}
            onClick={() => setSheetOpen(true)}
          />
          {!published ? (
            <p className="text-muted-foreground mt-2 text-xs">
              Publish the marketplace above first. The instructions reference
              its repo.
            </p>
          ) : null}
        </StepSection>

        <ConfirmTrafficSection
          index={3}
          description="Start a Cowork session and run any action. Its events show up here once the plugin is active for your organization."
          matchesSource={isCoworkSource}
        />
      </div>

      <PlatformInstrumentationSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        initialPlatformId={ANTHROPIC_ENTERPRISE_PLATFORM_ID}
        onPlatformStatusChange={(_, next) => setStatus(next)}
      />
    </StepContainer>
  );
}
