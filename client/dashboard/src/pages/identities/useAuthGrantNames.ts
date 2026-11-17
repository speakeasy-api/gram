import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { serverLabel } from "@/lib/auth-clients";
import { useAllAuthClients } from "@/lib/useAllAuthClients";
import { isAgentPolicyConstraint } from "@/pages/agents/agent-policy-grants";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { queryKeyOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useQueries, useQuery } from "@tanstack/react-query";

import {
  REPRESENTED_USER,
  type AuthGrantNames,
  type AuthGrantServer,
} from "./auth-grant-label";

/** The selector fields the names read. */
type AuthGrantSelector = {
  resourceKind: string;
  resourceId: string;
  connectionOwner?: string | undefined;
  upstreamClient?: string | undefined;
};

function authGrantServer(
  server: AuthClient["servers"][number],
): AuthGrantServer {
  return {
    id: server.id,
    slug: server.slug,
    name: serverLabel(server),
    projectId: server.projectId,
  };
}

/**
 * Names for the auth clients, upstream OAuth clients and people that the
 * agent's `mcp:auth` grants point at. Both lists need org:read and load only
 * when a grant needs them; without them the labels fall back to short ids.
 */
export function useAuthGrantNames(
  grants: { selector: AuthGrantSelector }[],
): AuthGrantNames {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const authGrants = grants.filter(
    ({ selector }) => selector.resourceKind === "mcp_auth_client",
  );
  const needsMembers = authGrants.some(
    ({ selector }) =>
      isAgentPolicyConstraint(selector.connectionOwner) &&
      selector.connectionOwner !== REPRESENTED_USER,
  );
  // Only a grant naming an auth client or an upstream OAuth client needs the
  // organization's auth clients; wildcard and type-only grants name neither.
  const needsClients = authGrants.some(
    ({ selector }) =>
      isAgentPolicyConstraint(selector.resourceId) ||
      isAgentPolicyConstraint(selector.upstreamClient),
  );
  const clients = useAllAuthClients({
    enabled: needsClients && hasScope("org:read"),
  });
  const upstreamIds = [
    ...new Set(
      authGrants.flatMap(({ selector }) =>
        isAgentPolicyConstraint(selector.upstreamClient)
          ? [selector.upstreamClient]
          : [],
      ),
    ),
  ];
  // Each upstream client a grant names, for its issuer (its page) and its own
  // name when no auth client signs in through it.
  const upstreamClients = useQueries({
    queries: upstreamIds.map((id) => ({
      queryKey: queryKeyOrganizationRemoteSessionClient({ id }),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.organizationRemoteSessionClients.get({ id }, undefined, {
          signal,
        }),
      enabled: hasScope("org:read"),
      retry: false,
      throwOnError: false,
    })),
  });
  const members = useQuery({
    queryKey: ["agent-auth-grant-members", organization.id],
    queryFn: ({ signal }) => sdk.access.listMembers({}, undefined, { signal }),
    enabled: needsMembers && hasScope("org:read"),
    retry: false,
    throwOnError: false,
  });
  const clientById = new Map(
    (clients.data ?? []).map((client) => [
      client.id,
      {
        name: client.name,
        clientType: client.clientType,
        servers: client.servers.map(authGrantServer),
      },
    ]),
  );
  const upstreamClientById = new Map(
    upstreamClients.flatMap(({ data }) => (data ? [[data.id, data]] : [])),
  );
  // An upstream OAuth client is named after the first auth client using it,
  // and reaches every server those auth clients serve.
  const upstreamById = new Map<
    string,
    { name: string; issuerId?: string; servers: AuthGrantServer[] }
  >();
  for (const client of clients.data ?? []) {
    if (!client.remoteSessionClientId) continue;
    const seen = upstreamById.get(client.remoteSessionClientId) ?? {
      name: client.name,
      issuerId: upstreamClientById.get(client.remoteSessionClientId)
        ?.remoteSessionIssuerId,
      servers: [],
    };
    for (const server of client.servers) {
      if (seen.servers.some(({ id }) => id === server.id)) continue;
      seen.servers.push(authGrantServer(server));
    }
    upstreamById.set(client.remoteSessionClientId, seen);
  }
  // One no auth client signs in through any more keeps its own name.
  for (const upstream of upstreamClientById.values()) {
    if (upstreamById.has(upstream.id)) continue;
    upstreamById.set(upstream.id, {
      name: remoteSessionClientDisplayName(upstream),
      issuerId: upstream.remoteSessionIssuerId,
      servers: [],
    });
  }
  const memberById = new Map(
    (members.data?.members ?? []).map((member) => [
      member.id,
      member.name || member.email,
    ]),
  );
  return {
    client: (id) => clientById.get(id),
    upstreamClient: (id) => upstreamById.get(id),
    member: (id) => memberById.get(id),
    clientsLoaded: clients.isSuccess,
  };
}
