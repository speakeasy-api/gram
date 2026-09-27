import { useState } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { MarketplaceSection } from "../marketplace-section";
import { isMarketplacePublished } from "../marketplace-status";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isAnthropicOrCursorSource } from "../hook-event-sources";
import { usePlatformApiKeys } from "../platform-setup-values";
import { PlatformSetupFlow } from "../platform-setup-flow";
import { platformStatusBadge } from "../platform-status-badge";
import type { PlatformSetupStatus } from "../../types";

interface AnthropicAdminControlsStepProps {
  onComplete: () => void;
}

export function AnthropicAdminControlsStep({
  onComplete,
}: AnthropicAdminControlsStepProps): JSX.Element {
  const apiKeys = usePlatformApiKeys({ shareAnthropicKey: true });
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >({});
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    throwOnError: false,
  });
  const published = isMarketplacePublished(publishStatus);
  // Claude.ai reads the marketplace repo through its own GitHub App, so a repo
  // nobody has been given access to is as unusable here as one that was never
  // published. A missing flag means the collaborator lookup failed, not that
  // there are none, so only a definite "no" holds these instructions back.
  const noCollaborators = publishStatus?.hasCollaborators === false;
  // Every path references the published marketplace. Only the org rollouts
  // that read the repo through GitHub (Cowork's GitHub App, Cursor's import)
  // need a collaborator; Claude Code clones through Speakeasy's proxy, and the
  // personal-plan paths use the proxy or a downloaded ZIP.
  const heldBack = published
    ? undefined
    : "Publish the marketplace above first — these instructions reference it.";
  const orgHeldBack = noCollaborators
    ? "Add a collaborator to the marketplace repo above first — the organization rollout cannot sync a repo nobody has access to."
    : undefined;

  const statusOf = (id: string): PlatformSetupStatus =>
    platformStatus[id] ?? "not_started";
  const setStatus = (id: string, next: PlatformSetupStatus) =>
    setPlatformStatus((prev) => ({ ...prev, [id]: next }));

  return (
    <StepContainer
      title="Set up Anthropic admin controls"
      description="Claude Cowork and Claude Code are both configured from Claude.ai: organization plugins make the observability plugin required in Cowork, which runs in Claude.ai's cloud sandbox out of the device agent's reach, and managed settings push it to Claude Code. Turn logging on, publish your plugin marketplace, connect each of them, optionally connect Cursor from the same marketplace, and confirm their events arrive."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <MarketplaceSection
          index={2}
          requiresCollaborators
          description="Claude.ai reads the observability plugin from your marketplace's GitHub repo: Cowork syncs the repo directly and managed settings reference its URL."
          publishedHint="Select this repo when Claude.ai asks which repository to sync."
        />

        <p className="text-muted-foreground text-sm">
          One Hooks API key is generated automatically and used for both Claude
          Code and Cowork.
        </p>

        <StepSection
          index={3}
          slug="connect-cowork"
          title="Connect Claude Cowork"
          description="On a Teams or Enterprise plan, Cowork syncs the marketplace repo through Claude's own GitHub App and the plugin is marked required from Organization settings. On a personal plan, each person uploads the plugin themselves."
          complete={statusOf("claude-cowork") === "complete"}
          aside={platformStatusBadge(statusOf("claude-cowork"))}
        >
          <PlatformSetupFlow
            apiKeys={apiKeys}
            platformId="claude-cowork"
            status={statusOf("claude-cowork")}
            onStatusChange={(next) => setStatus("claude-cowork", next)}
            heldBack={heldBack}
            orgHeldBack={orgHeldBack}
          />
        </StepSection>

        <StepSection
          index={4}
          slug="connect-claude-code"
          title="Connect Claude Code"
          description="On a Teams or Enterprise plan, managed settings on Claude.ai apply the marketplace and the observability plugin to every developer in your org, and the device agent, if you deploy it, also enforces the plugin on managed machines. On a personal plan, each developer adds the same settings to their own Claude Code."
          complete={statusOf("claude") === "complete"}
          aside={platformStatusBadge(statusOf("claude"))}
        >
          <PlatformSetupFlow
            apiKeys={apiKeys}
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
          description="On a Teams or Enterprise plan, Cursor's team marketplace imports the observability plugin from the same repo. On an individual plan, each person installs it as a local plugin. Skip this if your team doesn't use Cursor."
          complete={statusOf("cursor") === "complete"}
          aside={platformStatusBadge(statusOf("cursor"))}
        >
          <PlatformSetupFlow
            platformId="cursor"
            status={statusOf("cursor")}
            onStatusChange={(next) => setStatus("cursor", next)}
            heldBack={heldBack}
            orgHeldBack={orgHeldBack}
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
