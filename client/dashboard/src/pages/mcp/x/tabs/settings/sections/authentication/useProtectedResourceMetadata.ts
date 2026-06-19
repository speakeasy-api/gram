import { useSdkClient } from "@/contexts/Sdk";
import type { ProtectedResourceMetadata } from "@gram/client/models/components";
import { useQuery } from "@tanstack/react-query";

export type ProtectedResourceProbeStatus =
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
    queryKey: ["protected-resource-metadata", remoteMcpServerId],
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
