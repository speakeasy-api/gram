import { useState } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { MarketplaceSection } from "../marketplace-section";
import { isMarketplacePublished } from "../marketplace-status";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isAnthropicOrCursorSource } from "../hook-event-sources";
import { PlatformSetupFlow } from "../platform-setup-flow";
import { platformStatusBadge } from "../platform-status-badge";
import type { PlatformSetupStatus } from "../../types";

interface AnthropicObservabilityStepProps {
  onComplete: () => void;
}

export function AnthropicObservabilityStep({
  onComplete,
}: AnthropicObservabilityStepProps): JSX.Element {
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >({});
  const { data: publishStatus } = usePublishStatus();
  const published = isMarketplacePublished(publishStatus);
  const heldBack = published
    ? undefined
    : "Publish the marketplace above first — these instructions reference it.";

  const statusOf = (id: string): PlatformSetupStatus =>
    platformStatus[id] ?? "not_started";
  const setStatus = (id: string, next: PlatformSetupStatus) =>
    setPlatformStatus((prev) => ({ ...prev, [id]: next }));

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <AgentProviderIcon source="claude" className="h-6 w-6" />
        </div>
      }
      title="Set up Anthropic observability"
      description="Claude Cowork and Claude Code are both configured from Claude.ai: organization plugins make the observability plugin required in Cowork, which runs in Claude.ai's cloud sandbox out of the device agent's reach, and managed settings push it to Claude Code. Publish your plugin marketplace, connect each of them, optionally connect Cursor from the same marketplace, and confirm their events arrive."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <MarketplaceSection
          index={2}
          description="Claude.ai reads the observability plugin from your marketplace's GitHub repo: Cowork syncs the repo directly and managed settings reference its URL."
          publishedHint="Select this repo when Claude.ai asks which repository to sync."
        />

        <StepSection
          index={3}
          slug="connect-cowork"
          title="Connect Claude Cowork"
          description="Cowork syncs the marketplace repo through Claude's own GitHub App, so the plugin is marked required from Organization settings rather than pushed from a machine."
          complete={statusOf("claude-cowork") === "complete"}
          aside={platformStatusBadge(statusOf("claude-cowork"))}
        >
          <PlatformSetupFlow
            platformId="claude-cowork"
            status={statusOf("claude-cowork")}
            onStatusChange={(next) => setStatus("claude-cowork", next)}
            heldBack={heldBack}
          />
        </StepSection>

        <StepSection
          index={4}
          slug="connect-claude-code"
          title="Connect Claude Code"
          description="Managed settings on Claude.ai apply the marketplace and the observability plugin to every developer in your org. The device agent, if you deploy it, also enforces the plugin on managed machines."
          complete={statusOf("claude") === "complete"}
          aside={platformStatusBadge(statusOf("claude"))}
        >
          <PlatformSetupFlow
            platformId="claude"
            status={statusOf("claude")}
            onStatusChange={(next) => setStatus("claude", next)}
            heldBack={heldBack}
          />
        </StepSection>

        <StepSection
          index={5}
          slug="connect-cursor"
          title="Connect Cursor"
          badge="Optional"
          description="Cursor's team marketplace imports the observability plugin from the same repo. Skip this if your team doesn't use Cursor."
          complete={statusOf("cursor") === "complete"}
          aside={platformStatusBadge(statusOf("cursor"))}
        >
          <PlatformSetupFlow
            platformId="cursor"
            status={statusOf("cursor")}
            onStatusChange={(next) => setStatus("cursor", next)}
            heldBack={heldBack}
          />
        </StepSection>

        <ConfirmTrafficSection
          index={6}
          description="Run any tool in Claude Code or Cursor, or start a Cowork session. Their events show up here once the plugin is active."
          callout={{
            title: "Turn Cowork on before you chat",
            body: "The observability plugin's hooks run inside a Cowork session, not in an ordinary Claude conversation. On claude.ai, start a new conversation, switch the Claude Cowork toggle on, then send a message — its events land here.",
          }}
          matchesSource={isAnthropicOrCursorSource}
        />
      </div>
    </StepContainer>
  );
}
