import { SettingsSection } from "@/components/detail/settings-section";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthTarget } from "./authentication/authTarget";
import { UserSessionIssuerField } from "./authentication/UserSessionIssuerField";
import { useSelectableUserSessionIssuers } from "./authentication/useSelectableUserSessionIssuers";

export const MCP_SESSIONS_SECTION_ID = "sessions";

export function RemoteMcpSessionsSection({
  mcpServer,
  mcpUrl,
}: {
  mcpServer: McpServer;
  /** The URL clients connect to; undefined while it is unknown. */
  mcpUrl?: string;
}): JSX.Element {
  const target = useMcpServerAuthTarget(mcpServer, mcpUrl);
  const selectableIssuers = useSelectableUserSessionIssuers(target);

  // The provider's sign-in details and session settings live on its own
  // page in MCP Sessions, which the picker opens.
  return (
    <SettingsSection id={MCP_SESSIONS_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Sessions</SettingsSection.Title>
        <SettingsSection.Description>
          Choose where people sign in to this server. How long their sessions
          last and which clients may connect come with that choice.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <div className="divide-y">
          <UserSessionIssuerField
            target={target}
            issuers={selectableIssuers.issuers}
            isLoading={selectableIssuers.isLoading}
            isError={selectableIssuers.isError}
          />
        </div>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
