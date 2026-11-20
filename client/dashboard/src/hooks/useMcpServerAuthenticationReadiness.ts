import { useUpstreamProbe } from "@/lib/remote-identity";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";

/** Shared by the setup banner, server overview, and official catalog setup. */
export interface McpServerAuthenticationReadiness {
  ready: boolean;
  loading: boolean;
  /** A failed read is unknown, not an open setup step. */
  unavailable: boolean;
  description: string;
  upstreamAuthQuery: ReturnType<typeof useMcpServerAuthentication>;
  identityProbeStatus: ReturnType<typeof useUpstreamProbe>;
}

export function useMcpServerAuthenticationReadiness(
  mcpServer: McpServer | undefined,
): McpServerAuthenticationReadiness {
  const remoteMcpServerId = mcpServer?.remoteMcpServerId ?? "";
  const remote = remoteMcpServerId !== "";
  const tunneled = !!mcpServer?.tunneledMcpServerId;
  const unproxied = !!mcpServer?.unproxiedMcpServerId;
  const upstreamAuthQuery = useMcpServerAuthentication(
    { mcpServerId: mcpServer?.id ?? "" },
    undefined,
    { enabled: remote || tunneled, throwOnError: false },
  );
  const status = upstreamAuthQuery.data?.status;
  const identityProbeStatus = useUpstreamProbe(
    remoteMcpServerId,
    remote && !upstreamAuthQuery.isError && status === "no_auth_required",
  );
  const configured = !upstreamAuthQuery.isError && status === "configured";
  const acceptsAnonymous =
    !upstreamAuthQuery.isError &&
    status === "no_auth_required" &&
    identityProbeStatus === "available";

  let description =
    "Attach an authorization server so users can access the upstream service.";
  if (unproxied) {
    description =
      "Not applicable — the customer connects directly using the vendor's own credentials.";
  } else if (remote) {
    description = configured
      ? "Credentials are set up for the upstream service."
      : acceptsAnonymous
        ? "The upstream service accepts requests without credentials."
        : "Add a credential so Speakeasy can sign in to the upstream service.";
  } else if (tunneled) {
    description =
      "Speakeasy authentication is configured; upstream authorization servers are optional.";
  }

  return {
    ready:
      unproxied ||
      (remote && (configured || acceptsAnonymous)) ||
      (tunneled && !!mcpServer?.userSessionIssuerId),
    loading:
      remote &&
      !upstreamAuthQuery.isError &&
      (upstreamAuthQuery.isPending || identityProbeStatus === "loading"),
    unavailable:
      remote &&
      (upstreamAuthQuery.isError || identityProbeStatus === "unknown"),
    description,
    upstreamAuthQuery,
    identityProbeStatus,
  };
}
