import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { ClientCard } from "./ClientCard";
import { ClientLifecycleSections } from "./ClientLifecycleSections";
import { IdentityProviderCard } from "./IdentityProviderCard";

export function OverviewTab({
  client,
  issuer,
  isIssuerLoading,
  issuerId,
}: {
  client: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  isIssuerLoading: boolean;
  issuerId: string;
}): JSX.Element {
  return (
    <div className="flex max-w-3xl flex-col gap-6">
      {(issuer || isIssuerLoading) && (
        <IdentityProviderCard issuerId={issuerId} issuer={issuer} />
      )}
      <ClientCard client={client} issuer={issuer} issuerId={issuerId} />
      <ClientLifecycleSections
        client={client}
        issuer={issuer}
        issuerId={issuerId}
      />
    </div>
  );
}
