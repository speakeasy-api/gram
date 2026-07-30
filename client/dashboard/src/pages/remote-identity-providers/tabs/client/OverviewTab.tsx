import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useOrganizationRemoteSessionClientMcpServers } from "@gram/client/react-query/organizationRemoteSessionClientMcpServers.js";
import { useOrganizationRemoteSessionClientSessions } from "@gram/client/react-query/organizationRemoteSessionClientSessions.js";
import { useMemo } from "react";
import { remoteSessionClientDisplayName } from "../../clientDisplay";
import { InfoField, InfoSection, InfoText } from "../../detailFields";
import { issuerDisplayName } from "../../issuerDisplay";
import { OAuthFlowDiagram, type ClientFlowData } from "../../OAuthFlowDiagram";
import { formatTimestamp } from "./formatTimestamp";

export function OverviewTab({
  client,
  issuer,
}: {
  client: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
}): JSX.Element {
  // Fetch MCP servers and sessions for flow counts
  const { data: mcpServersData } = useOrganizationRemoteSessionClientMcpServers(
    { clientId: client.id },
  );
  const { data: sessionsData } = useOrganizationRemoteSessionClientSessions({
    clientId: client.id,
  });

  const flowData: ClientFlowData | null = useMemo(() => {
    if (!issuer) return null;
    return {
      issuerName: issuerDisplayName(issuer),
      issuerUrl: issuer.issuer,
      clientName: remoteSessionClientDisplayName(client),
      clientId: client.clientId,
      mcpServerCount: mcpServersData?.items?.length ?? 0,
      sessionCount: sessionsData?.result.items?.length ?? 0,
    };
  }, [issuer, client, mcpServersData, sessionsData]);

  const scope =
    client.scope && client.scope.length > 0 ? client.scope.join(", ") : "—";

  return (
    <div className="max-w-3xl space-y-8">
      {flowData && (
        <section>
          <h3 className="text-muted-foreground mb-3 text-xs font-medium tracking-wide uppercase">
            OAuth Flow
          </h3>
          <OAuthFlowDiagram variant="client" data={flowData} />
        </section>
      )}

      <div className="grid items-start gap-8 sm:grid-cols-2">
        <InfoSection title="Essentials">
          <InfoField label="Client ID">
            <InfoText mono>{client.clientId}</InfoText>
          </InfoField>
          <InfoField label="Client Issued At">
            <InfoText>{formatTimestamp(client.clientIdIssuedAt)}</InfoText>
          </InfoField>
        </InfoSection>

        <InfoSection title="Details">
          <InfoField label="Audience">
            <InfoText mono>{client.audience || "—"}</InfoText>
          </InfoField>
          <InfoField label="Scope">
            <InfoText mono>{scope}</InfoText>
          </InfoField>
          <InfoField label="Token Endpoint Authentication Method">
            <InfoText mono>
              {client.tokenEndpointAuthMethod ?? "client_secret_basic"}
            </InfoText>
          </InfoField>
        </InfoSection>
      </div>
    </div>
  );
}
