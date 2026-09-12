import { useSdkClient } from "@/contexts/Sdk";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useQuery } from "@tanstack/react-query";

export type RemoteMcpAuthenticationProbeStatus =
  | "idle"
  | "loading"
  | "authentication-required"
  | "available"
  | "unknown";

export function useRemoteMcpAuthenticationProbe(
  remoteMcpServerId: string,
  enabled: boolean,
): RemoteMcpAuthenticationProbeStatus {
  const client = useSdkClient();
  const sourceQuery = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: enabled && remoteMcpServerId !== "", throwOnError: false },
  );
  const url = sourceQuery.data?.url;
  const probeQuery = useQuery({
    queryKey: ["remote-mcp-authentication-probe", remoteMcpServerId, url],
    queryFn: async () => {
      if (!url) throw new Error("remote MCP URL is unavailable");
      return client.remoteMcp.probeURL({
        probeURLForm: { url },
      });
    },
    enabled: enabled && !!url,
    retry: false,
    staleTime: 5 * 60 * 1000,
    throwOnError: false,
  });

  if (!enabled || remoteMcpServerId === "") return "idle";
  if (sourceQuery.isLoading || probeQuery.isLoading) return "loading";
  if (sourceQuery.isError || probeQuery.isError || !probeQuery.data) {
    return "unknown";
  }

  switch (probeQuery.data.outcome) {
    case "authentication_required":
      return "authentication-required";
    case "mcp_available":
      return "available";
    case "invalid_mcp_response":
    case "unreachable":
      return "unknown";
  }
}
