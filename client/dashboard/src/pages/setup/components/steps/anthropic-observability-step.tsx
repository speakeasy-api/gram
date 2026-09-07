import { useState } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { AGENT_PROVIDERS } from "@/components/agent-providers/agent-providers";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { MarketplaceSection } from "../marketplace-section";
import { isMarketplacePublished } from "../marketplace-status";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isAnthropicSource } from "../hook-event-sources";
import { AgentPlatformPickerItem } from "../agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "../platform-instrumentation-sheet";
import { platformStatusBadge } from "../platform-status-badge";
import { ANTHROPIC_PLATFORM_IDS } from "../../setup-data";
import type { PlatformSetupStatus } from "../../types";

interface AnthropicObservabilityStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function AnthropicObservabilityStep({
  onComplete,
  onBack,
}: AnthropicObservabilityStepProps): JSX.Element {
  const [sheetPlatformId, setSheetPlatformId] = useState<string | null>(null);
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >({});
  const { data: publishStatus } = usePublishStatus();
  const published = isMarketplacePublished(publishStatus);
  const connectedCount = ANTHROPIC_PLATFORM_IDS.filter(
    (id) => platformStatus[id] === "complete",
  ).length;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <AgentProviderIcon source="claude" className="h-6 w-6" />
        </div>
      }
      title="Set up Anthropic observability"
      description="Claude Code and Claude Cowork are both configured from Claude.ai: managed settings push the observability plugin to Claude Code, and organization plugins make it required in Cowork, which runs in Claude.ai's cloud sandbox out of the device agent's reach. Publish your plugin marketplace, connect both, and confirm their events arrive."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-8">
        <MarketplaceSection
          index={1}
          description="Claude.ai reads the observability plugin from your marketplace's GitHub repo: managed settings reference its URL and Cowork syncs the repo directly."
          publishedHint="Select this repo when Claude.ai asks which repository to sync."
        />

        <StepSection
          index={2}
          title="Connect Claude Code and Claude Cowork"
          description="Each opens step-by-step instructions for the matching Claude.ai admin page. The device agent, if you deploy it, also enforces the plugin on managed machines."
          complete={connectedCount === ANTHROPIC_PLATFORM_IDS.length}
          aside={
            <span className="text-muted-foreground text-xs">
              {connectedCount} of {ANTHROPIC_PLATFORM_IDS.length} connected
            </span>
          }
        >
          <div className="space-y-3">
            {ANTHROPIC_PLATFORM_IDS.map((id) => {
              const provider = AGENT_PROVIDERS[id];
              const status = platformStatus[id] ?? "not_started";
              return (
                <AgentPlatformPickerItem
                  key={id}
                  platformId={id}
                  name={provider.name}
                  description={provider.description}
                  complete={status === "complete"}
                  statusBadge={platformStatusBadge(status)}
                  disabled={!published}
                  onClick={() => setSheetPlatformId(id)}
                />
              );
            })}
          </div>
          {!published ? (
            <p className="text-muted-foreground mt-2 text-xs">
              Publish the marketplace above first. Both sets of instructions
              reference it.
            </p>
          ) : null}
        </StepSection>

        <ConfirmTrafficSection
          index={3}
          description="Run any tool in Claude Code or start a Cowork session. Their events show up here once the plugin is active."
          matchesSource={isAnthropicSource}
        />
      </div>

      <PlatformInstrumentationSheet
        open={!!sheetPlatformId}
        onOpenChange={(open) => {
          if (!open) setSheetPlatformId(null);
        }}
        initialPlatformId={sheetPlatformId ?? undefined}
        onPlatformStatusChange={(id, next) =>
          setPlatformStatus((prev) => ({ ...prev, [id]: next }))
        }
      />
    </StepContainer>
  );
}
