import { SettingsSection } from "@/components/detail/settings-section";
import { SharedTunnelImpact } from "@/components/mcp/shared-tunnel-impact";
import { useSharedTunnelImpact } from "@/components/mcp/use-shared-tunnel-impact";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { useTelemetry } from "@/contexts/Telemetry";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import { TUNNELED_MCP_FEATURE_FLAG } from "@/lib/tunneledMcp";
import { addMcpServerOnTunnelHref } from "@/pages/sources/tunneled-mcp/existingTunnel";
import { TunneledMcpSetupTabs } from "@/pages/sources/tunneled-mcp/TunneledMcpSetupTabs";
import { useRoutes } from "@/routes";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { Plus } from "lucide-react";
import { Link } from "react-router";

export const MCP_AGENT_SETUP_SECTION_ID = "agent-setup";

// The create flow shows these snippets once with the plaintext key. People
// come back for them when they add an agent host or rebuild one, so the same
// tabs live here with the key prefix in place of the key.
export function AgentSetupSection({
  tunneledMcpServer,
  mcpServerId,
}: {
  tunneledMcpServer: TunneledMcpServer;
  /** The MCP server whose settings page renders this section. */
  mcpServerId: string;
}): JSX.Element {
  const routes = useRoutes();
  const impact = useSharedTunnelImpact(tunneledMcpServer.id, { active: true });
  // The add page redirects away while tunneled MCP is off for the org.
  const canAddOnTunnel =
    useTelemetry().isFeatureEnabled(TUNNELED_MCP_FEATURE_FLAG) === true;

  return (
    <SettingsSection id={MCP_AGENT_SETUP_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Agent Setup</SettingsSection.Title>
        <SettingsSection.Description>
          Run a tunnel agent next to the upstream MCP server to connect this
          source. The snippets use a placeholder where the tunnel key goes; the
          key itself was shown once when it was issued or last rotated.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <SharedTunnelImpact
            impact={impact}
            tunnelName={formatTunneledMcpDisplay(tunneledMcpServer)}
            currentMcpServerId={mcpServerId}
            effect="One agent serves them all, so restarting or reconfiguring it affects every one."
            publicWarning={tunneledMcpServer.allowPublic}
          />
          <TunneledMcpSetupTabs
            serverName={tunneledMcpServer.name}
            keyPrefix={tunneledMcpServer.keyPrefix}
          />
        </SettingsSection.Body>
        {canAddOnTunnel ? (
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Add another MCP server on this tunnel, for example with different
              settings or visibility. No new key or agent is needed.
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              <RequireScope
                scope="mcp:write"
                resourceId={tunneledMcpServer.projectId}
                projectId={tunneledMcpServer.projectId}
                level="component"
              >
                <Button variant="secondary" size="md" asChild>
                  <Link
                    to={addMcpServerOnTunnelHref(
                      routes.mcp.add.tunneled.href(),
                      tunneledMcpServer.id,
                    )}
                  >
                    <Button.LeftIcon>
                      <Plus className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Add MCP server on this tunnel</Button.Text>
                  </Link>
                </Button>
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        ) : null}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
