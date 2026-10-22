import { useState } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { MarketplaceSection } from "../marketplace-section";
import { marketplaceHoldBacks } from "../marketplace-status";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { isAnthropicSource } from "../hook-event-sources";
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
  // Only Cowork's org rollout reads the repo through GitHub; Claude Code clones
  // through Speakeasy's proxy, and the personal-plan paths use the proxy or a
  // downloaded ZIP.
  const { heldBack, orgHeldBack } = marketplaceHoldBacks(publishStatus);

  const statusOf = (id: string): PlatformSetupStatus =>
    platformStatus[id] ?? "not_started";
  const setStatus = (id: string, next: PlatformSetupStatus) =>
    setPlatformStatus((prev) => ({ ...prev, [id]: next }));

  return (
    <StepContainer
      title="Set up Anthropic admin controls"
      description="On Team or Enterprise, an Owner or Primary Owner can configure Cowork organization plugins and Claude Code server-managed settings from Claude.ai. Personal installations and endpoint-managed policies are separate paths. Turn logging on, publish your plugin marketplace, follow the appropriate setup below, and verify actual event delivery."
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
          description="On Team or Enterprise, an Owner or Primary Owner syncs the marketplace through Claude's GitHub App and marks the plugin Required. On Pro or Max, each person can add a personal marketplace (including private GitHub) or upload a ZIP. Required installation and successful upload do not prove hook execution."
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
          description="On Team or Enterprise, an Owner or Primary Owner configures Claude.ai server-managed settings for eligible signed-in Claude Code sessions. Endpoint-managed policy files and MDM are separate mechanisms. Personal setup uses each developer's user settings, which higher-precedence policy can override. Start a new session and verify logs, metrics, and beta traces."
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

        <ConfirmTrafficSection
          index={5}
          description="Run any tool in Claude Code, or start a Cowork session. Confirm their events actually arrive here; installation alone does not prove delivery."
          callout={{
            title: "Turn Cowork on before you chat",
            body: "Start a new Cowork session and run a tool, rather than an ordinary Claude conversation where hooks are ignored. Verify received events on the surface and version you use; upload success does not prove hook execution or network access.",
          }}
          matchesSource={isAnthropicSource}
        />
      </div>
    </StepContainer>
  );
}
