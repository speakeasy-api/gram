import { SettingsSection } from "@/components/detail/settings-section";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useAllRemoteSessionClients } from "@/lib/remote-identity";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";

import { useMcpServerAuthTarget } from "./authTarget";
import { IdentityChainingField } from "./IdentityChainingField";
import { identityChainingProviders } from "./identityChainingGate";
import { useSpeakeasyIssuerStatus } from "./useSpeakeasyIssuerStatus";

export const IDENTITY_CHAINING_SECTION_ID = "identity-chaining";

/**
 * Its own section because it acts on its own (each action applies at once)
 * rather than through the Identity panel's Save.
 */
export function IdentityChainingSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element | null {
  const target = useMcpServerAuthTarget(mcpServer);
  const remoteMcpServerId = target.remoteMcpServerId ?? "";
  const userSessionIssuerId = target.userSessionIssuerId ?? undefined;
  const { data: userSessionIssuer } = useUserSessionIssuer(
    { id: userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId, throwOnError: false },
  );
  const { items: clients, isLoading: clientsLoading } =
    useAllRemoteSessionClients(
      { userSessionIssuerId },
      { enabled: !!userSessionIssuerId },
    );
  const sourceQuery = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "", throwOnError: false },
  );
  const externalFlag = useFeatureFlag(FEATURE_FLAGS.oktaConnections);
  const emaProvidersFlag = useFeatureFlag(FEATURE_FLAGS.emaProviders);
  const speakeasyIssuer = useSpeakeasyIssuerStatus();
  const providers = identityChainingProviders(userSessionIssuer, {
    externalRollout: externalFlag.status === "enabled",
    speakeasyRollout: emaProvidersFlag.status === "enabled",
    speakeasyIssuer: speakeasyIssuer.data,
  });
  const resource = sourceQuery.data?.url;

  if (!userSessionIssuer || !providers || !resource || clientsLoading) {
    return null;
  }

  return (
    <SettingsSection id={IDENTITY_CHAINING_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Identity chaining</SettingsSection.Title>
        <SettingsSection.Description>
          Gets people into the service behind this server with a sign-in they
          already have, from your identity provider or Speakeasy, so they
          don&apos;t connect it separately.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <IdentityChainingField
          userSessionIssuer={userSessionIssuer}
          providers={providers}
          speakeasyIssuerUrl={speakeasyIssuer.data?.issuer}
          speakeasyEnabled={speakeasyIssuer.data?.state === "enabled"}
          linkedClients={clients}
          resource={resource}
          remoteMcpServerId={remoteMcpServerId}
        />
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
