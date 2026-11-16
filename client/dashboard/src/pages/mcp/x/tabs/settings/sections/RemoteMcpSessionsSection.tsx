import { SettingsSection } from "@/components/detail/settings-section";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { useMcpServerAuthTarget } from "./authentication/authTarget";
import { UserIdentitySessionControls } from "./authentication/UserIdentitySessionControls";
import { SharedAuthorizationServerField } from "./authentication/SharedAuthorizationServerField";
import { UserSessionIssuerField } from "./authentication/UserSessionIssuerField";
import { useSelectableUserSessionIssuers } from "./authentication/useSelectableUserSessionIssuers";
import { useAllRemoteSessionClients } from "@/lib/remote-identity";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";

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
  const mcpAuthenticationPage =
    useFeatureFlag(FEATURE_FLAGS.userSessionIssuerDashboard).status ===
    "enabled";
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
      <SettingsSection.Body>
        <Skeleton label="Loading session settings">
          <div className="h-5 w-48" />
          <div className="h-9 w-full max-w-md" />
        </Skeleton>
      </SettingsSection.Body>
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
          {/* The provider's sign-in details and session settings live on
              its own page, which the picker opens. */}
          {!mcpAuthenticationPage && issuerQuery.data ? (
            <SharedAuthorizationServerField
              userSessionIssuer={issuerQuery.data}
              mcpUrl={mcpUrl}
            />
          ) : null}
          {mcpAuthenticationPage ? null : sessionControls}
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
