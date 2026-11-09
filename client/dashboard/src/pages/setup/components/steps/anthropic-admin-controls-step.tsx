import { useMemo, useState } from "react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { getCoworkTeamSteps } from "../../setup-data";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { MarketplaceSection } from "../marketplace-section";
import { marketplaceHoldBacks } from "../marketplace-status";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { OrganizationGatewayConnectorSection } from "../organization-gateway-section";
import { isAnthropicSource } from "../hook-event-sources";
import { usePlatformApiKeys } from "../platform-setup-values";
import { PlatformSetupFlow } from "../platform-setup-flow";
import { PlatformStepList } from "../platform-step-list";
import { platformStatusBadge } from "../platform-status-badge";
import type { PlatformSetupStatus } from "../../types";

interface AnthropicAdminControlsStepProps {
  onComplete: () => void;
}

export function AnthropicAdminControlsStep({
  onComplete,
}: AnthropicAdminControlsStepProps): JSX.Element {
  const apiKeys = usePlatformApiKeys({ shareAnthropicKey: true });
  const [claudeCodeStatus, setClaudeCodeStatus] =
    useState<PlatformSetupStatus>("not_started");
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    throwOnError: false,
  });
  // Claude Code clones the marketplace through Speakeasy's proxy, so it needs
  // a published marketplace but no GitHub collaborator. Registering the plugins
  // for Claude Desktop, which syncs through GitHub, is its own card.
  const { heldBack } = marketplaceHoldBacks(publishStatus);
  const { monitoring } = useMemo(() => getCoworkTeamSteps(), []);

  return (
    <StepContainer
      title="Set up Anthropic admin controls"
      description="On Team or Enterprise, an Owner or Primary Owner configures Claude Code server-managed settings and Cowork's native monitoring from Claude.ai. Personal installations and endpoint-managed policies are separate paths. Registering Speakeasy's plugins for Claude Desktop and the organization instruction that captures Cowork conversations have their own cards under Set up Claude Desktop. Turn logging on, publish your plugin marketplace, follow the setup below, optionally add the Organization Gateway as a Claude connector, and verify actual event delivery."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <MarketplaceSection
          index={2}
          description="Claude Code's server-managed settings reference your marketplace's URL."
        />

        <p className="text-muted-foreground text-sm">
          One Hooks API key is generated automatically and used for both Claude
          Code and Cowork.
        </p>

        <StepSection
          index={3}
          slug="connect-claude-code"
          title="Connect Claude Code"
          description="On Team or Enterprise, an Owner or Primary Owner configures Claude.ai server-managed settings for eligible signed-in Claude Code sessions. Endpoint-managed policy files and MDM are separate mechanisms. Personal setup uses each developer's user settings, which higher-precedence policy can override. Start a new session and verify logs, metrics, and beta traces."
          complete={claudeCodeStatus === "complete"}
          aside={platformStatusBadge(claudeCodeStatus)}
        >
          <PlatformSetupFlow
            apiKeys={apiKeys}
            platformId="claude"
            status={claudeCodeStatus}
            onStatusChange={setClaudeCodeStatus}
            heldBack={heldBack}
          />
        </StepSection>

        <StepSection
          index={4}
          slug="connect-cowork"
          title="Turn on Cowork monitoring"
          description="Sends Cowork usage, tokens and cost to Speakeasy."
        >
          <PlatformStepList
            platformId="claude-cowork"
            steps={[monitoring]}
            apiKeys={apiKeys}
          />
        </StepSection>

        <StepSection
          index={5}
          slug="connect-organization-gateway"
          title="Add the Organization Gateway to Claude"
          badge="Optional"
          description="The Organization Gateway gives each person every MCP server they can reach at one URL. On Team or Enterprise, an Owner adds it once as an organization connector; on Pro or Max, each person adds it as a custom connector. Its URL appears once an admin has reviewed MCP assignments and turned the gateway on."
        >
          <OrganizationGatewayConnectorSection />
        </StepSection>

        <ConfirmTrafficSection
          index={6}
          description="Run a tool in Claude Code and check its events arrive here."
          callout={{
            title: "Check Cowork in Agent Sessions",
            body: "Cowork conversations don't appear in this list. Look for them in Agent Sessions after Claude's first reply.",
          }}
          matchesSource={isAnthropicSource}
        />
      </div>
    </StepContainer>
  );
}
