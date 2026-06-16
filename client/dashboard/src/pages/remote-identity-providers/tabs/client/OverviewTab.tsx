import type { RemoteSessionClient } from "@gram/client/models/components";
import { InfoField, InfoSection, InfoText } from "../../detailFields";
import { formatTimestamp } from "./formatTimestamp";

export function OverviewTab({
  client,
}: {
  client: RemoteSessionClient;
}): JSX.Element {
  const scope =
    client.scope && client.scope.length > 0 ? client.scope.join(", ") : "—";

  return (
    <div className="grid max-w-3xl items-start gap-8 sm:grid-cols-2">
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
  );
}
