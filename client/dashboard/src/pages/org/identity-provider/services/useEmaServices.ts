import { useQuery } from "@tanstack/react-query";
import type { EMAService } from "@gram/client/models/components/emaservice.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useIdentityProviderConnection } from "@gram/client/react-query/identityProviderConnection.js";
import { buildOrganizationRemoteSessionIssuerEMAServicesQuery } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.core.js";
import { useOrganization } from "@/contexts/Auth";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";

import { SESSION_SECURITY } from "../identityProviderQueries";
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

/** Keyed by organization so switching organizations never shows the last one's services. */
function emaServicesQueryKey(
  organizationId: string,
): readonly [string, string, string, { organizationId: string }] {
  return [
    "@gram/client",
    "organizationRemoteSessionIssuers",
    "listEMAServices",
    { organizationId },
  ] as const;
}

/** The Okta connection's issuer record, so a broken Okta provider links to Okta and nothing else does. */
export function useOktaIssuerId(enabled = true): string | undefined {
  const query = useIdentityProviderConnection(undefined, SESSION_SECURITY, {
    enabled,
    throwOnError: false,
    retry: false,
  });
  return query.data?.connection?.remoteSessionIssuerId;
}

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
  const organization = useOrganization();
  const client = useGramContext();
  const query = useQuery({
    ...buildOrganizationRemoteSessionIssuerEMAServicesQuery(
      client,
      undefined,
      SESSION_SECURITY,
    ),
    queryKey: emaServicesQueryKey(organization.id),
    throwOnError: false,
    retry: false,
  });
  const speakeasy = useEnterpriseIssuer();
  const oktaIssuerId = useOktaIssuerId();
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
      oktaIssuerId,
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

function serviceRow(
  service: EMAService,
  {
    issuerById,
    speakeasyIssuerUrl,
    oktaIssuerId,
  }: {
    issuerById: (id: string | undefined) => RemoteSessionIssuer | undefined;
    speakeasyIssuerUrl: string | undefined;
    oktaIssuerId: string | undefined;
  },
): ServiceRow {
  return {
    key: service.vendorIssuer,
    vendorIssuer: service.vendorIssuer,
    name: serviceName(service.name, service.vendorIssuer),
    remoteSessionIssuerId: service.remoteSessionIssuerId,
    isDefaultOrder: service.emaProviders.isDefault,
    providers: service.emaProviders.providers.map((provider) =>
      providerView(provider, {
        issuer: issuerById(provider.remoteSessionIssuerId),
        speakeasyIssuerUrl,
        oktaIssuerId,
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
