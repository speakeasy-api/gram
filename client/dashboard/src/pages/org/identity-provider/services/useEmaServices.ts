import type { EMAService } from "@gram/client/models/components/emaservice.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useOrganizationRemoteSessionIssuerEMAServices } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import { useEnterpriseIssuer } from "../useEnterpriseIssuer";
import { providerView, serviceName, type ProviderView } from "./emaServiceView";

export type ServiceRow = {
  key: string;
  vendorIssuer: string;
  name: string;
  remoteSessionIssuerId?: string;
  isDefaultOrder: boolean;
  providers: ProviderView[];
  serverCount: number;
  readyServerCount: number;
};

/**
 * Every service identity chaining reaches in the organization, with each
 * identity provider named and judged the same way the server page does.
 */
export function useEmaServices(): {
  rows: ServiceRow[];
  isPending: boolean;
  isError: boolean;
  isFetching: boolean;
  refetch: () => void;
  issuerById: (id: string | undefined) => RemoteSessionIssuer | undefined;
} {
  const query = useOrganizationRemoteSessionIssuerEMAServices(
    undefined,
    { sessionHeaderGramSession: "" },
    { throwOnError: false, retry: false },
  );
  const speakeasy = useEnterpriseIssuer();
  const items = query.data?.items ?? [];
  const externalIds = [
    ...new Set(
      items.flatMap((service) =>
        service.emaProviders.providers.flatMap((provider) =>
          provider.kind === "external" && provider.remoteSessionIssuerId
            ? [provider.remoteSessionIssuerId]
            : [],
        ),
      ),
    ),
  ];
  const issuers = useRemoteSessionIssuersByIds(externalIds);
  const issuerById = (id: string | undefined) =>
    issuers.items.find((issuer) => issuer.id === id);
  const rows = items.map((service) =>
    serviceRow(service, {
      issuerById,
      speakeasyIssuerUrl: speakeasy.data?.issuer,
    }),
  );
  return {
    rows,
    isPending: query.isPending,
    isError: query.isError,
    isFetching: query.isFetching,
    refetch: () => void query.refetch(),
    issuerById,
  };
}

export function serviceRow(
  service: EMAService,
  {
    issuerById,
    speakeasyIssuerUrl,
  }: {
    issuerById: (id: string | undefined) => RemoteSessionIssuer | undefined;
    speakeasyIssuerUrl: string | undefined;
  },
): ServiceRow {
  return {
    key: service.vendorIssuer,
    vendorIssuer: service.vendorIssuer,
    name: serviceName(service.name, service.vendorIssuer),
    remoteSessionIssuerId: service.remoteSessionIssuerId,
    isDefaultOrder: service.emaProviders.isDefault,
    // An organization-wide order stands for no one server, so the default's
    // external placeholder (each server's own identity provider) has no id.
    providers: service.emaProviders.providers.map((provider) =>
      providerView(provider, {
        issuer: issuerById(provider.remoteSessionIssuerId),
        speakeasyIssuerUrl,
      }),
    ),
    serverCount: service.serverCount,
    readyServerCount: service.readyServerCount,
  };
}

/** How many servers reach the service, and how many of those are ready. */
export function serversSummary(row: ServiceRow): string {
  if (row.serverCount === 0) return "None yet";
  const servers =
    row.serverCount === 1 ? "1 server" : `${row.serverCount} servers`;
  if (row.readyServerCount === row.serverCount) return servers;
  return `${servers} · ${row.readyServerCount} ready`;
}
