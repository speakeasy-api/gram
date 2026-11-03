import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient";
import type { Gram } from "@gram/client";
import { queryKeyServerClientAssociations } from "@gram/client/react-query/serverClientAssociations";
import { queryKeyRemoteSessionClient } from "@gram/client/react-query/remoteSessionClient";

function associationOptions(sdk: Gram, serverId: string) {
  return {
    queryKey: queryKeyServerClientAssociations({ mcpServerId: serverId }),
    queryFn: () =>
      sdk.remoteSessions.listServerClientAssociations({
        mcpServerId: serverId,
      }),
    staleTime: 0,
  };
}
function clientOptions(sdk: Gram, id: string) {
  return {
    queryKey: queryKeyRemoteSessionClient({ id }),
    queryFn: () => sdk.remoteSessionClients.get({ id }),
    staleTime: 0,
  };
}
function matchesIssuer(value: string, issuer: string): boolean {
  try {
    const url = new URL(value);
    return (
      url.origin === issuer &&
      url.pathname.replace(/\/+$/, "") === "" &&
      !url.search &&
      !url.hash &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}

type ClientConfiguration = { client: RemoteSessionClient; serverCount: number };

/** A secret-free configuration view backed by the same queries as Identity Settings. */
export function useServerClientConfiguration(
  serverId: string,
  issuer: string,
): {
  data: ClientConfiguration | null;
  isSuccess: boolean;
  isLoading: boolean;
  isError: boolean;
  error: Error | null;
  refetch: () => Promise<ClientConfiguration | null>;
} {
  const sdk = useSdkClient();
  const cache = useQueryClient();
  const associations = useQuery({
    ...associationOptions(sdk, serverId),
    retry: false,
    throwOnError: false,
  });
  const association = associations.data?.associations.find((a) =>
    matchesIssuer(a.issuerUrl, issuer),
  );
  const client = useQuery({
    ...clientOptions(sdk, association?.remoteSessionClientId ?? ""),
    enabled: !!association,
    retry: false,
    throwOnError: false,
  });
  // Sensitive writes need a fresh read even when Settings has populated the cache.
  const refetch = useCallback(async () => {
    const associations = associationOptions(sdk, serverId);
    // Do not join a request started before an identity/configuration change.
    await cache.cancelQueries({ queryKey: associations.queryKey, exact: true });
    const fresh = await cache.fetchQuery(associations);
    const association = fresh.associations.find((a) =>
      matchesIssuer(a.issuerUrl, issuer),
    );
    if (!association) return null;
    const client = clientOptions(sdk, association.remoteSessionClientId);
    await cache.cancelQueries({ queryKey: client.queryKey, exact: true });
    return {
      client: await cache.fetchQuery(client),
      serverCount: association.serverCount,
    };
  }, [cache, sdk, serverId, issuer]);
  return {
    data:
      association && client.data
        ? { client: client.data, serverCount: association.serverCount }
        : null,
    isSuccess: associations.isSuccess && (!association || client.isSuccess),
    isLoading: associations.isPending || (!!association && client.isPending),
    isError: associations.isError || (!!association && client.isError),
    error: associations.error ?? (association ? client.error : null),
    refetch,
  };
}
