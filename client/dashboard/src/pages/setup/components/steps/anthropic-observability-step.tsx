import { useState } from "react";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { StepContainer } from "../step-container";
import { AgentPlatformPickerItem } from "../agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "../platform-instrumentation-sheet";
import { platformStatusBadge } from "../platform-status-badge";
import { AGENT_PLATFORMS, ANTHROPIC_PLATFORM_IDS } from "../../setup-data";
import type { PlatformSetupStatus } from "../../types";

interface AnthropicObservabilityStepProps {
  onComplete: () => void;
  onBack: () => void;
}

const ANTHROPIC_PLATFORMS = ANTHROPIC_PLATFORM_IDS.map((id) =>
  AGENT_PLATFORMS.find((platform) => platform.id === id)!,
);

export function AnthropicObservabilityStep({
  onComplete,
}: AnthropicObservabilityStepProps): JSX.Element {
  const [sheetPlatformId, setSheetPlatformId] = useState<string | null>(null);
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >({});
  const completedCount = ANTHROPIC_PLATFORMS.filter(
    (platform) => platformStatus[platform.id] === "complete",
  ).length;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <AgentProviderIcon source="claude" className="h-6 w-6" />
        </div>
      }
      title="Set up Anthropic observability"
      description="Configure Claude Code and Claude Cowork from Claude.ai so their activity is visible in Speakeasy."
      onContinue={onComplete}
    >
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground text-sm">
            {completedCount} of {ANTHROPIC_PLATFORMS.length} platforms
            configured
          </span>
        </div>

        {ANTHROPIC_PLATFORMS.map((platform) => {
          const status = platformStatus[platform.id] ?? "not_started";
          return (
            <AgentPlatformPickerItem
              key={platform.id}
              platformId={platform.id}
              name={platform.name}
              description={platform.description}
              complete={status === "complete"}
              statusBadge={platformStatusBadge(status)}
              onClick={() => setSheetPlatformId(platform.id)}
            />
          );
        })}

        {sheetPlatformId && (
          <PlatformInstrumentationSheet
            key={sheetPlatformId}
            open={!!sheetPlatformId}
            onOpenChange={(open) => {
              if (!open) setSheetPlatformId(null);
            }}
            initialPlatformId={sheetPlatformId ?? undefined}
            onPlatformStatusChange={(id, status) =>
              setPlatformStatus((prev) => ({ ...prev, [id]: status }))
            }
          />
        )}
      </div>
    </StepContainer>
  );
}
