import { SettingsSection } from "@/components/detail/settings-section";
import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { UserIdentitySessionControls } from "./authentication/UserIdentitySessionControls";
import { useAllRemoteSessionClients } from "./authentication/useAllRemoteSessionClients";

export function RemoteMcpSessionsSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const userSessionIssuerId = mcpServer.userSessionIssuerId ?? undefined;
  const issuerQuery = useUserSessionIssuer(
    { id: userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId, throwOnError: false },
  );
  const clientsQuery = useAllRemoteSessionClients(
    { userSessionIssuerId },
    { enabled: !!userSessionIssuerId, throwOnError: false },
  );
  const userIdentityConfigured = clientsQuery.items.length > 0;
  const loading =
    (!!userSessionIssuerId && !issuerQuery.data && issuerQuery.isLoading) ||
    (clientsQuery.isLoading && !userIdentityConfigured);
  const failed =
    (issuerQuery.isError && !issuerQuery.data) ||
    (clientsQuery.isError && !userIdentityConfigured);

  let content: JSX.Element;
  if (!userSessionIssuerId) {
    content = (
      <Text muted>
        Session controls apply when User Identity is configured for this server.
      </Text>
    );
  } else if (loading) {
    content = <Text muted>Loading session settings...</Text>;
  } else if (failed || !issuerQuery.data) {
    content = (
      <Text className="text-destructive">
        Session settings could not be loaded.
      </Text>
    );
  } else if (!userIdentityConfigured) {
    content = (
      <Text muted>
        Session controls apply when User Identity is configured for this server.
      </Text>
    );
  } else {
    return (
      <SettingsSection>
        <SessionsHeader />
        <SettingsSection.Panel>
          <div className="divide-y">
            <UserIdentitySessionControls userSessionIssuer={issuerQuery.data} />
          </div>
        </SettingsSection.Panel>
      </SettingsSection>
    );
  }

  return (
    <SettingsSection>
      <SessionsHeader />
      <SettingsSection.Panel>
        <SettingsSection.Body>{content}</SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function SessionsHeader(): JSX.Element {
  return (
    <SettingsSection.Header>
      <SettingsSection.Title>Sessions</SettingsSection.Title>
      <SettingsSection.Description>
        Control how long User Identity connections last and which MCP clients
        may initiate them.
      </SettingsSection.Description>
    </SettingsSection.Header>
  );
}
