import { useQuery } from "@tanstack/react-query";
import type { EMAService } from "@gram/client/models/components/emaservice.js";
import type { EMAServiceServer } from "@gram/client/models/components/emaserviceserver.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildIdentityProviderConnectionQuery } from "@gram/client/react-query/identityProviderConnection.core.js";
import { buildOrganizationRemoteSessionIssuerEMAServicesQuery } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.core.js";
import { useOrganization } from "@/contexts/Auth";
import { pluralize } from "@/lib/format";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";

import { SESSION_SECURITY } from "../identityProviderQueries";
import { useEnterpriseIssuer } from "../useEnterpriseIssuer";
import { providerView, serviceName, type ProviderView } from "./emaServiceView";

export type ServiceRow = {
  key: string;
  vendorIssuer: string;
  name: string;
  remoteSessionIssuerId?: string;
  isDefault: boolean;
  providers: ProviderView[];
  /** Every server that reaches the service, including ones the viewer can't see. */
  serverCount: number;
  readyServerCount: number;
  /** The servers the viewer can see. */
  servers: EMAServiceServer[];
  /** Servers the viewer can't see; counted, never named. */
  hiddenServerCount: number;
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

/**
 * The Okta connection's issuer record, so a broken Okta provider links to
 * Okta and nothing else does. Keyed by organization under the SDK's prefix,
 * so switching organizations never reuses the last one's connection and the
 * SDK's invalidation still reaches it.
 */
export function useOktaIssuerId(enabled = true): string | undefined {
  const organization = useOrganization();
  const client = useGramContext();
  const base = buildIdentityProviderConnectionQuery(
    client,
    undefined,
    SESSION_SECURITY,
  );
  const query = useQuery({
    ...base,
    queryKey: [...base.queryKey, { organizationId: organization.id }],
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
    isDefault: service.emaProviders.isDefault,
    providers: service.emaProviders.providers.map((provider) =>
      providerView(provider, {
        issuer: issuerById(provider.remoteSessionIssuerId),
        speakeasyIssuerUrl,
        oktaIssuerId,
      }),
    ),
    serverCount: service.serverCount,
    readyServerCount: service.readyServerCount,
    servers: service.servers,
    hiddenServerCount: service.hiddenServerCount,
  };
}

/** How many MCP servers reach the service, and how many of those are ready. */
export function serversSummary(row: ServiceRow): string {
  if (row.serverCount === 0) return "No MCP servers yet";
  const servers = pluralize(row.serverCount, "MCP server");
  if (row.readyServerCount === row.serverCount) return servers;
  return `${servers} · ${row.readyServerCount} ready`;
}
