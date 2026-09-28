import { useSdkClient } from "@/contexts/Sdk";
import type { ProtectedResourceMetadata } from "@gram/client/models/components/protectedresourcemetadata.js";
import { useQuery, type QueryClient } from "@tanstack/react-query";

const PROTECTED_RESOURCE_METADATA_QUERY_KEY = "protected-resource-metadata";

// The probe is keyed by remote id but answers for whatever URL the remote had
// when it ran, so a URL change must drop it. A reset rather than an
// invalidation: the hook below only reports isLoading, so a cached answer
// left in place during the refetch would keep "Use discovered" armed with the
// old upstream's authorization server until the new probe lands.
export function resetAllProtectedResourceMetadata(
  queryClient: QueryClient,
): Promise<void> {
  return queryClient.resetQueries({
    queryKey: [PROTECTED_RESOURCE_METADATA_QUERY_KEY],
  });
}

type ProtectedResourceProbeStatus =
  | "idle"
  | "loading"
  | "available"
  | "unavailable";

export type ProtectedResourceProbeResult = {
  status: ProtectedResourceProbeStatus;
  metadata: ProtectedResourceMetadata | null;
};

// Calls remoteMcp.discoverProtectedResourceMetadata instead of probing from
// the browser. The endpoint runs the RFC 9728 probe under guardian.Policy and
// returns HTTP 200 with available=false for any probe-level failure (404,
// CORS-N/A, malformed, transport error, etc.), so we only treat transport /
// server errors here as "unavailable" — normal upstream unavailability flows
// through the success path.
export function useProtectedResourceMetadata(
  remoteMcpServerId: string | undefined,
  enabled: boolean,
): ProtectedResourceProbeResult {
  const client = useSdkClient();

  const query = useQuery({
    queryKey: [PROTECTED_RESOURCE_METADATA_QUERY_KEY, remoteMcpServerId],
    queryFn: async () => {
      if (!remoteMcpServerId) throw new Error("no remote mcp server id");
      return client.remoteMcp.discoverProtectedResourceMetadata({
        discoverProtectedResourceMetadataRequestBody: {
          remoteMcpServerId,
        },
      });
    },
    enabled: enabled && !!remoteMcpServerId,
    retry: false,
    staleTime: 5 * 60 * 1000,
  });

  if (!enabled || !remoteMcpServerId) {
    return { status: "idle", metadata: null };
  }
  if (query.isLoading) {
    return { status: "loading", metadata: null };
  }
  if (
    query.isError ||
    !query.data ||
    !query.data.available ||
    !query.data.metadata
  ) {
    return { status: "unavailable", metadata: null };
  }
  return { status: "available", metadata: query.data.metadata };
}
