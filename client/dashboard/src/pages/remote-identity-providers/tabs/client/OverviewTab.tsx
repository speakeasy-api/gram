import { DetailList } from "@/components/ui/detail-list";
import { Heading } from "@/components/ui/heading";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { ReactNode } from "react";
import { formatTimestamp } from "./formatTimestamp";

function Section({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div>
      <Heading variant="h4" className="mb-3">
        {title}
      </Heading>
      <DetailList orientation="stacked">{children}</DetailList>
    </div>
  );
}

export function OverviewTab({
  client,
}: {
  client: RemoteSessionClient;
}): JSX.Element {
  const scope =
    client.scope && client.scope.length > 0 ? client.scope.join(", ") : "—";

  return (
    <div className="grid max-w-3xl items-start gap-8 sm:grid-cols-2">
      <Section title="Essentials">
        <DetailList.Item
          label="Client ID"
          value={<span className="font-mono break-all">{client.clientId}</span>}
        />
        <DetailList.Item
          label="Client Issued At"
          value={formatTimestamp(client.clientIdIssuedAt)}
        />
      </Section>

      <Section title="Details">
        <DetailList.Item
          label="Audience"
          value={
            <span className="font-mono break-all">
              {client.audience || "—"}
            </span>
          }
        />
        <DetailList.Item
          label="Scope"
          value={<span className="font-mono break-all">{scope}</span>}
        />
        <DetailList.Item
          label="Token Endpoint Authentication Method"
          value={
            <span className="font-mono break-all">
              {client.tokenEndpointAuthMethod ?? "client_secret_basic"}
            </span>
          }
        />
      </Section>
    </div>
  );
}
