import { SettingsSection } from "@/components/detail/settings-section";
import { TunneledMcpSetupTabs } from "@/pages/sources/tunneled-mcp/TunneledMcpSetupTabs";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";

export const MCP_AGENT_SETUP_SECTION_ID = "agent-setup";

// The create flow shows these snippets once with the plaintext key. People
// come back for them when they add an agent host or rebuild one, so the same
// tabs live here with the key prefix in place of the key.
export function AgentSetupSection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}): JSX.Element {
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
          <TunneledMcpSetupTabs
            serverName={tunneledMcpServer.name}
            keyPrefix={tunneledMcpServer.keyPrefix}
          />
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
