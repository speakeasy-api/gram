import { SettingsSection } from "@/components/detail/settings-section";
import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { useMcpServerAuthTarget } from "./authentication/authTarget";
import { UserIdentitySessionControls } from "./authentication/UserIdentitySessionControls";
import { UserSessionIssuerField } from "./authentication/UserSessionIssuerField";
import { useSelectableUserSessionIssuers } from "./authentication/useSelectableUserSessionIssuers";
import { useAllRemoteSessionClients } from "@/lib/remote-identity";

export function RemoteMcpSessionsSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const target = useMcpServerAuthTarget(mcpServer);
  const selectableIssuers = useSelectableUserSessionIssuers(target);
  const userSessionIssuerId = mcpServer.userSessionIssuerId ?? undefined;
  const issuerQuery = useUserSessionIssuer(
    { id: userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId, throwOnError: false },
  );
  const clientsQuery = useAllRemoteSessionClients(
    { userSessionIssuerId },
    { enabled: !!userSessionIssuerId },
  );
  const loading = issuerQuery.isLoading || clientsQuery.isLoading;
  const failed = issuerQuery.isError || clientsQuery.isError;
  const userIdentityConfigured = clientsQuery.items.length > 0;

  let sessionControls: JSX.Element;
  if (loading) {
    sessionControls = (
      <SessionsNotice>Loading session settings...</SessionsNotice>
    );
  } else if (failed || !issuerQuery.data) {
    sessionControls = (
      <SettingsSection.Body>
        <Text className="text-destructive">
          Session settings could not be loaded.
        </Text>
      </SettingsSection.Body>
    );
  } else if (!userIdentityConfigured) {
    sessionControls = (
      <SessionsNotice>
        Session controls apply when User Identity is configured for this server.
      </SessionsNotice>
    );
  } else {
    sessionControls = (
      <UserIdentitySessionControls userSessionIssuer={issuerQuery.data} />
    );
  }

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Sessions</SettingsSection.Title>
        <SettingsSection.Description>
          Control how long User Identity connections last and which MCP clients
          may initiate them.
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
          {sessionControls}
        </div>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function SessionsNotice({ children }: { children: string }): JSX.Element {
  return (
    <SettingsSection.Body>
      <Text muted>{children}</Text>
    </SettingsSection.Body>
  );
}
